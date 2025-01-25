package be_cluster

import (
	"errors"
	"io"
	"net"
	"syscall"
	"time"
)

var (
	IllegalStateError = errors.New("illegal state. unexpected read from socket")
)

func checkConn(conn net.Conn) error {
	_ = conn.SetDeadline(time.Time{})
	sysConn, ok := conn.(syscall.Conn)
	if !ok {
		return nil
	}
	rawConn, err := sysConn.SyscallConn()
	if err != nil {
		return err
	}
	var sysErr error
	// read data from the socket buffer. check if the connection is still alive
	if err := rawConn.Read(func(fd uintptr) bool {
		var buf [1]byte
		n, err := syscall.Read(int(fd), buf[:])
		switch {
		case n == 0 && err == nil:
			sysErr = io.EOF
		case n > 0:
			sysErr = IllegalStateError
		case errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK):
			sysErr = nil
		default:
			sysErr = err
		}
		return true
	}); err != nil {
		return err
	}
	return sysErr
}
