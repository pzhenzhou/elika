package be_cluster

import (
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lithammer/shortuuid/v4"
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/pzhenzhou/elika/pkg/respio"
)

const (
	DefaultQueueSize = 128
)

type TxState struct {
	TxBeginCmd   []byte
	OwnerSession *Session
	// OwnerId string
	State respio.TxCmdStateType
}

var (
	logger       = common.InitLogger().WithName("backend")
	drainTimeout = 500 * time.Millisecond
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
	writeQ  chan *RequestContext
	quit    chan struct{}
	txState *TxState
	closed  atomic.Bool
	created time.Time
	usedAt  int64
	txLock  sync.RWMutex
	wg      sync.WaitGroup
	// instanceId field to track which backend instance this connection belongs to
	instanceId string
}

func NewBackendConn(timeout time.Duration, addr string, queueSize int) (*BackendConn, error) {
	dialer := &net.Dialer{
		Timeout: timeout,
		Control: func(network, address string, c syscall.RawConn) error {
			var ctrlErr error
			err := c.Control(func(fd uintptr) {
				// Set SO_REUSEADDR to avoid "address already in use" errors
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
					ctrlErr = fmt.Errorf("failed to set SO_REUSEADDR: %w", err)
					logger.Error(ctrlErr, "Failed to set SO_REUSEADDR")
					return
				}
				// Optionally set SO_REUSEPORT (if available on your platform)
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
					ctrlErr = fmt.Errorf("failed to set SO_REUSEPORT: %w", err)
					logger.Error(ctrlErr, "Failed to set SO_REUSEPORT")
					return
				}
			})
			if err != nil {
				return err
			}
			return ctrlErr
		},
	}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		logger.Error(err, "Failed to new backend", "Addr", addr)
		return nil, err
	}
	serverConn := &BackendConn{
		Id:         shortuuid.New(),
		created:    time.Now(),
		conn:       conn,
		reader:     respio.NewRespReader(conn),
		writer:     respio.NewRespWriter(conn),
		writeQ:     make(chan *RequestContext, queueSize),
		quit:       make(chan struct{}, 2),
		pendingQ:   make(chan *RequestContext, queueSize),
		wg:         sync.WaitGroup{},
		txLock:     sync.RWMutex{},
		instanceId: addr,
		closed:     atomic.Bool{},
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
				errorPacket := respio.AcquireRespPacket()
				errorPacket.Type = respio.RespError
				errorPacket.Data = []byte(err.Error())

				pCtx.Session.OutQ <- &ResponseContext{
					Response: errorPacket,
				}
				continue
			}
			bc.pendingQ <- pCtx
		default:
			// logger.Info("WriteQ is empty")
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
			// logger.Info("PendingQ is empty")
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
	//logger.Info("BackendConn WriteAndFlush flush complete.",
	//	"packet", pkt, "Id", bc.Id, "FlushErr", flushErr)
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
			// logger.Info("BackendConn WriteLoop packet", "packet", pCtx.Request, "Id", bc.Id)
			if err := bc.WriteAndFlush(pCtx.Request); err != nil {
				logger.Error(err, "BackendConn Failed to write packet")
				pCtx.Session.OutQ <- NewErrResponseContext(err)
				if common.IsBackendUnavailable(err) {
					logger.Info("BackendConn WriteLoop connection closed", "error", err)
					bc.Clear()
					return
				}
				continue
			}
			currTxState := bc.LoadTxnState()
			if currTxState == nil {
				bc.pendingQ <- pCtx
			}
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
			packet, err := bc.reader.Read()
			// logger.Info("BackendConn ReadLoop packet", "packet", packet, "Id", bc.Id)
			if err != nil {
				if common.IsBackendUnavailable(err) {
					logger.Info("BackendConn ReadLoop connection closed", "error", err)
					bc.Clear()
					return
				}
				continue
			}
			currTxState := bc.LoadTxnState()
			if currTxState != nil && currTxState.OwnerSession != nil {
				currTxState.OwnerSession.OutQ <- &ResponseContext{
					Response: packet,
				}
				if currTxState.State == respio.TxCmdStateEnd {
					bc.ClearTxnState()
				}
				continue
			}

			pCtx := <-bc.pendingQ
			rspCtx := &ResponseContext{
				Response: packet,
			}
			// Safely check if session has auth info with password
			authInfo := pCtx.Session.GetAuthInfo()
			if authInfo != nil && authInfo.Password == nil {
				// Update the complete auth info in the session
				// This preserves the username set earlier but adds the password
				// The username is needed for routing, the password for re-authentication
				if pCtx.Request.IsAuthCmd() && packet.Type == respio.RespStatus &&
					bytes.Equal(packet.Data, respio.OkCmd) {
					rspCtx.Callback = func(session *Session) {
						existingAuthInfo := session.GetAuthInfo()
						if existingAuthInfo != nil {
							session.SetAuthInfo(&common.AuthInfo{
								Username: existingAuthInfo.Username,
								Password: pCtx.AuthInfo.Password,
							})
						} else {
							session.SetAuthInfo(pCtx.AuthInfo)
						}
					}
				} else if pCtx.Request.IsAuthCmd() {
					// auth failed
					logger.Info("BackendConn ReadLoop auth failed", "packet", packet, "Id", bc.Id)
				}
			}
			pCtx.Session.OutQ <- rspCtx
		}
	}
}

func (bc *BackendConn) LoadTxnState() *TxState {
	bc.txLock.RLock()
	defer bc.txLock.RUnlock()
	return bc.txState
}

func (bc *BackendConn) UpdateTxnState(session *Session, stateType respio.TxCmdStateType) {
	bc.txLock.Lock()
	defer bc.txLock.Unlock()
	bc.txState = &TxState{
		OwnerSession: session,
		State:        stateType,
	}
}

func (bc *BackendConn) ClearTxnState() {
	bc.txLock.Lock()
	defer bc.txLock.Unlock()
	bc.txState = nil
}

func (bc *BackendConn) Buffered() int {
	return bc.reader.Buffered()
}

func (bc *BackendConn) drainQueues() {
	bc.drainWriteQ()
	bc.drainPendingQ()
}

func (bc *BackendConn) Clear() {
	if !bc.closed.Swap(true) {
		close(bc.quit)
		bc.drainQueues()
		bc.innerClose()
	}
}

func (bc *BackendConn) innerClose() {
	if bc.conn != nil {
		closeErr := bc.conn.Close()
		logger.Info("BackendConn connection closed", "connId", bc.Id, "error", closeErr)
	}
}

func (bc *BackendConn) Close() error {
	bc.Clear()
	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		bc.wg.Wait()
		done <- struct{}{}
	}()
	select {
	case <-done:
		logger.Info("BackendConn shutdown completed", "connId", bc.Id)
		return nil
	case <-time.After(1 * time.Second):
		logger.Info("BackendConn shutdown timed out", "connId", bc.Id)
		return nil
	}
}

func (bc *BackendConn) UsedAt() time.Time {
	sec := atomic.LoadInt64(&bc.usedAt)
	return time.Unix(sec, 0)
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

func (bc *BackendConn) EnsureAuth(authPacket *respio.RespPacket, sessionAuthInfo *common.AuthInfo) (*respio.RespPacket, error) {
	// Create a channel for the response
	responseCh := make(chan struct {
		resp *respio.RespPacket
		err  error
	}, 1)
	// Create a tmp session struct that will capture the response
	authSession := &Session{
		Id:   "auth_" + bc.Id,
		OutQ: make(chan *ResponseContext, 1),
	}
	reqCtx := &RequestContext{
		Session:  authSession,
		Request:  authPacket,
		AuthInfo: sessionAuthInfo,
	}
	go func() {
		select {
		case rspCtx := <-authSession.OutQ:
			resp := rspCtx.Response
			responseCh <- struct {
				resp *respio.RespPacket
				err  error
			}{resp, nil}
		case <-time.After(500 * time.Millisecond):
			responseCh <- struct {
				resp *respio.RespPacket
				err  error
			}{nil, errors.New("authentication timeout")}
		}
	}()
	bc.Enqueue(reqCtx)
	result := <-responseCh
	return result.resp, result.err
}
