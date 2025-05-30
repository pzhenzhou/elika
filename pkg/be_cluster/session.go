package be_cluster

import (
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/pzhenzhou/elika/pkg/respio"
	"net"
	"sync/atomic"
)

const (
	DefaultSessionOutQSize = 1024
)

// Session represents the TCP connection between a client and the ProxyServer.
// Memory:
//   - Id string: ~24-32 bytes (16 bytes for string header + 8-16 bytes for content)
//   - Client net.Conn: ~80-120 bytes (interface + TCPConn struct)
//   - authInfo atomic.Value: ~16 bytes
//   - quit chan struct{}: ~16 bytes (channel header)
//   - OutQ chan *respio.RespPacket: ~16 bytes (channel header)
//     Note: RespPacket pointers in channel are temporary and removed after sending
//   - reader *respio.RespReader: ~24 bytes
//     → 8 bytes (pointer) + ~16 bytes (*bufio.Reader overhead)
//   - writer *respio.RespWriter: ~24 bytes
//     → 8 bytes (pointer) + ~16 bytes (*bufio.Writer overhead)
//
// Total: ~200-248 bytes base size
// Note: Buffer memory (DefaultBufferSize = 8KB) is allocated in the underlying
// Memory Usage Estimation:
// +------------------+---------------+----------------+
// | Connections      | Total Objects | Memory Usage   |
// +------------------+---------------+----------------+
// | 10K              | 10,000        | ~2.48 MB       |
// | 100K             | 100,000       | ~24.8 MB       |
// | 1M               | 1,000,000     | ~248 MB        |
// | 10M              | 10,000,000    | ~2.48 GB       |
// +------------------+---------------+----------------+
type Session struct {
	Id       string
	Client   net.Conn
	authInfo atomic.Value
	quit     chan struct{}
	OutQ     chan *ResponseContext
	reader   *respio.RespReader
	writer   *respio.RespWriter
}

func NewSession(Id string, client net.Conn, queueSize int) *Session {
	return &Session{
		Id:     Id,
		Client: client,
		quit:   make(chan struct{}),
		OutQ:   make(chan *ResponseContext, queueSize),
		reader: respio.NewRespReader(client),
		writer: respio.NewRespWriter(client),
	}
}

func (s *Session) Read() (*respio.RespPacket, error) {
	return s.reader.Read()
}

func (s *Session) ReadBuffered() int {
	return s.reader.Buffered()
}

func (s *Session) WriteAndFlush(pkt *respio.RespPacket) error {
	err := s.writer.Write(pkt)
	if err != nil {
		logger.Error(err, "Failed to write packet to client", "SessionId", s.Id)
		return err
	}
	return s.writer.Flush()
}

func (s *Session) ReplyLoop() {
	for {
		select {
		case <-s.quit:
			// logger.Info("Session ReadLoop stop", "Id", s.Id)
			return
		case rspCtx := <-s.OutQ:
			respPacket := rspCtx.Response
			callback := rspCtx.Callback
			if callback != nil {
				callback(s)
			}
			if err := s.WriteAndFlush(respPacket); err != nil {
				logger.Error(err, "Failed to write packet to client", "SessionId", s.Id)
				// Release the packet even if there was an error writing it
				respio.ReleaseRespPacket(respPacket)
				continue
			}
			// Release the packet back to the pool after successfully writing it
			respio.ReleaseRespPacket(respPacket)
		}
	}
}

func (s *Session) Close() {
	logger.Info("Session close", "Id", s.Id)
	select {
	case <-s.quit: // Already closed
		return
	default:
		close(s.quit)
	}
}

func (s *Session) IsAuthenticated() bool {
	return s.GetAuthInfo() != nil
}

func (s *Session) SetAuthInfo(authInfo *common.AuthInfo) {
	s.authInfo.Store(authInfo)
}

func (s *Session) GetAuthInfo() *common.AuthInfo {
	if authInfo := s.authInfo.Load(); authInfo != nil {
		return authInfo.(*common.AuthInfo)
	}
	return nil
}
