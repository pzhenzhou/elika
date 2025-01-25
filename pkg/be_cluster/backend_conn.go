package be_cluster

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lithammer/shortuuid/v4"
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/pzhenzhou/elika/pkg/respio"
)

const (
	KvPortName = "kv-port"

	DefaultQueueSize = 1024
)

type TxState struct {
	OwnerId string
	State   respio.TxCmdStateType
}

var (
	ErrNoEndpoint = errors.New("BackendConn: No endpoint found")
	ErrNotAuthCmd = errors.New("BackendConn: Not an AUTH command")
	logger        = common.InitLogger().WithName("backend")
	drainTimeout  = 500 * time.Millisecond
)

type BackendConn struct {
	Id     string
	conn   net.Conn
	reader *respio.RespReader
	writer *respio.RespWriter
	// pendingQ :The commands that have been sent to the backend but not yet match the response.
	// A typical pattern is a FIFO structure that you push new request.
	pendingQ chan *RequestContext
	// writeQ:  The commands that need to conn forwarded to backend.
	writeQ   chan *RequestContext
	quit     chan struct{}
	txState  *TxState
	authInfo atomic.Value
	closed   atomic.Bool
	created  time.Time
	usedAt   int64
	txLock   sync.RWMutex
	wg       sync.WaitGroup
}

func NewBackendConn(timeout time.Duration, addr string) (*BackendConn, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		logger.Error(err, "Failed to new backend", "Addr", addr)
		return nil, err
	}
	serverConn := &BackendConn{
		Id:       shortuuid.New(),
		created:  time.Now(),
		conn:     conn,
		authInfo: atomic.Value{},
		reader:   respio.NewRespReader(conn),
		writer:   respio.NewRespWriter(conn),
		writeQ:   make(chan *RequestContext, DefaultQueueSize),
		quit:     make(chan struct{}, 1),
		pendingQ: make(chan *RequestContext, DefaultQueueSize),
		wg:       sync.WaitGroup{},
		txLock:   sync.RWMutex{},
		closed:   atomic.Bool{},
	}
	serverConn.wg.Add(2)
	serverConn.start()
	return serverConn, nil
}

func (bc *BackendConn) start() {
	go bc.WriteLoop()
	go bc.ReadLoop()
}

func (bc *BackendConn) drainWriteQ() {
	timeout := time.After(drainTimeout)
	for {
		select {
		case <-timeout:
			logger.Info("BackendConn drain write timeout")
			return
		case pCtx, ok := <-bc.writeQ:
			if !ok {
				return
			}
			if err := bc.WriteAndFlush(pCtx.Request); err != nil {
				pCtx.Session.OutQ <- &ResponseContext{
					Response: &respio.RespPacket{
						Type: respio.RespError,
						Data: []byte(err.Error()),
					},
				}
				continue
			}
			bc.pendingQ <- pCtx
		default:
			logger.Info("WriteQ is empty")
			return
		}
	}
}

func (bc *BackendConn) drainPendingQ() {
	timeout := time.After(drainTimeout)
	for {
		select {
		case <-timeout:
			logger.Info("BackendConn drain pending timeout")
			return
		case pCtx, ok := <-bc.pendingQ:
			if !ok {
				return
			}
			packet, err := bc.reader.Read()
			if err != nil {
				pCtx.Session.OutQ <- NewErrResponseContext(err)
				continue
			}
			pCtx.Session.OutQ <- &ResponseContext{
				Response: packet,
			}
		default:
			logger.Info("PendingQ is empty")
			return
		}
	}
}

func (bc *BackendConn) WriteAndFlush(pkt *respio.RespPacket) error {
	err := bc.writer.Write(pkt)
	if err != nil {
		logger.Error(err, "BackendConn Failed to write packet")
		return err
	}
	return bc.writer.Flush()
}

func (bc *BackendConn) Enqueue(pCtx *RequestContext) {
	bc.writeQ <- pCtx
}

func (bc *BackendConn) WriteLoop() {
	defer func() {
		bc.wg.Done()
		logger.Info("BackendConn WriteLoop done")
	}()
	for {
		select {
		case <-bc.quit:
			logger.Info("BackendConn WriteLoop quit")
			return
		case pCtx, ok := <-bc.writeQ:
			if !ok {
				return
			}
			if err := bc.WriteAndFlush(pCtx.Request); err != nil {
				logger.Error(err, "BackendConn Failed to write packet")
				pCtx.Session.OutQ <- NewErrResponseContext(err)
				continue
			}
			bc.pendingQ <- pCtx
		}
	}
}

func (bc *BackendConn) ReadLoop() {
	defer func() {
		bc.wg.Done()
		logger.Info("BackendConn ReadLoop done")
	}()
	for {
		select {
		case <-bc.quit:
			logger.Info("BackendConn ReadLoop quit")
			return
		default:
			// TODO : add ReadTimeout connection closed.
			packet, err := bc.reader.Read()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					logger.Info("BackendConn ReadLoop connection closed")
				} else {
					logger.Error(err, "BackendConn Failed to read packet")
				}
				continue
			}
			pCtx := <-bc.pendingQ
			rspCtx := &ResponseContext{
				Response: packet,
			}
			isAuthenticated := bc.IsAuthenticated()
			if !isAuthenticated {
				// logger.Info("BackendConn not authenticated", "BackendId", bc.Id, "ClintId", pCtx.Session.Id)
				authInfo := pCtx.AuthInfo
				if pCtx.Request.IsAuthCmd() {
					if packet.Type == respio.RespStatus && string(packet.Data) == "OK" {
						bc.SetAuthInfo(authInfo)
					}
				}
				rspCtx.Callback = func(session *Session) {
					session.SetAuthInfo(authInfo)
				}
			} else {
				if !pCtx.Session.IsAuthenticated() {
					if packet.Type == respio.RespStatus && string(packet.Data) == "OK" {
						rspCtx.Callback = func(session *Session) {
							session.SetAuthInfo(bc.LoadAuthInfo())
						}
					}
				}
			}
			pCtx.Session.OutQ <- rspCtx
		}
	}
}

func (bc *BackendConn) LoadAuthInfo() *common.AuthInfo {
	if info := bc.authInfo.Load(); info != nil {
		return info.(*common.AuthInfo)
	} else {
		return nil
	}
}

func (bc *BackendConn) IsAuthenticated() bool {
	return bc.authInfo.Load() != nil
}

func (bc *BackendConn) SetAuthInfo(info *common.AuthInfo) {
	if info != nil {
		bc.authInfo.Store(info)
	}
}

func (bc *BackendConn) LoadTxnState() *TxState {
	bc.txLock.RLock()
	defer bc.txLock.RUnlock()
	return bc.txState
}

func (bc *BackendConn) UpdateTxnState(id string, stateType respio.TxCmdStateType) {
	bc.txLock.Lock()
	defer bc.txLock.Unlock()
	bc.txState = &TxState{
		OwnerId: id,
		State:   stateType,
	}
}

func (bc *BackendConn) Buffered() int {
	return bc.reader.Buffered()
}

func (bc *BackendConn) drainQueues() {
	bc.drainWriteQ()
	bc.drainPendingQ()
}

func (bc *BackendConn) Clear() {
	close(bc.quit)
	bc.closed.Store(true)
	bc.drainQueues()
}

func (bc *BackendConn) innerClose() {
	if bc.conn != nil {
		_ = bc.conn.Close()
	}
}

func (bc *BackendConn) Close() error {
	bc.Clear()
	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		bc.wg.Wait()
		logger.Info("BackendConn goroutines shutdown completed", "connId", bc.Id)
		done <- struct{}{}
	}()

	defer bc.innerClose()
	select {
	case <-done:
		logger.Info("BackendConn shutdown completed", "connId", bc.Id)
		return nil
	case <-time.After(2 * time.Second):
		logger.Info("BackendConn shutdown timed out", "connId", bc.Id)
		return nil
	}
}

func (bc *BackendConn) UsedAt() time.Time {
	unix := atomic.LoadInt64(&bc.usedAt)
	return time.Unix(unix, 0)
}

func (bc *BackendConn) SetUsedAt(inTime time.Time) {
	atomic.StoreInt64(&bc.usedAt, inTime.Unix())
}

func (bc *BackendConn) RemoteAddr() net.Addr {
	if bc.conn != nil {
		return bc.conn.RemoteAddr()
	}
	return nil
}

func (bc *BackendConn) EnsureAuth(authPacket *respio.RespPacket) (*respio.RespPacket, error) {
	if !authPacket.IsAuthCmd() {
		logger.Info("Not an AUTH command", "authPacket", authPacket)
		return nil, ErrNotAuthCmd
	}
	if bc.IsAuthenticated() {
		return respio.OkStatus, nil
	}
	// Write auth command
	if err := bc.WriteAndFlush(authPacket); err != nil {
		return nil, err
	}
	resp, err := bc.reader.Read()
	if err != nil {
		return nil, err
	}
	if resp.Type == respio.RespStatus && string(resp.Data) == "OK" {
		authInfo := authPacket.ToAuthInfo()
		bc.SetAuthInfo(authInfo)
	}
	return resp, nil
}
