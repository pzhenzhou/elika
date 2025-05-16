package common

import (
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"math/rand"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

const (
	// Memory-related constants
	_  = iota
	KB = 1 << (10 * iota)
	MB
	GB
)

const (
	DefaultMaxMemory = 512 * MB
)

const (
	TenantKeySeparator = '.'
	EncodingAlphabet   = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-_"
	ProxyRuntime       = "PROXY_RUNTIME"
)

func RawZapLogger() *zap.Logger {
	logConfig := zap.Config{
		Level:             zap.NewAtomicLevelAt(zap.DebugLevel),
		Development:       true,
		DisableCaller:     false,
		DisableStacktrace: false,
		Encoding:          "console",
		OutputPaths: []string{
			"stderr",
		},
		ErrorOutputPaths: []string{
			"stderr",
		},
	}
	encoderCfg := zap.NewDevelopmentEncoderConfig()
	if IsProdRuntime() {
		logConfig.Development = false
		logConfig.Encoding = "json"
		logConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
		encoderCfg = zap.NewProductionEncoderConfig()
	}
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	logConfig.EncoderConfig = encoderCfg
	zapLogger, initLogErr := logConfig.Build()
	if initLogErr != nil {
		panic(fmt.Sprintf("Failed to initialize zap logger %v", initLogErr))
	}
	return zapLogger
}

func InitLogger() logr.Logger {
	zapLogger := RawZapLogger()
	return zapr.NewLogger(zapLogger)
}

func IsProdRuntime() bool {
	runEvnVal, hasEnv := os.LookupEnv(ProxyRuntime)
	if hasEnv {
		return strings.Compare(strings.ToLower(runEvnVal), "prod") == 0
	} else {
		return false
	}
}

func DecodeBase62(s string) (uint64, error) {
	var decoded uint64
	for i := len(s) - 1; i >= 0; i-- {
		pos := strings.IndexByte(EncodingAlphabet, s[i])
		if pos == -1 {
			return 0, fmt.Errorf("invalid character in tenant key")
		}
		decoded = decoded*62 + uint64(pos)
	}
	return decoded, nil
}

// EncodeBase62 encoding/decoding functions
func EncodeBase62(n uint64) string {
	if n == 0 {
		return string(EncodingAlphabet[0])
	}

	var encoded strings.Builder
	for n > 0 {
		encoded.WriteByte(EncodingAlphabet[n%62])
		n /= 62
	}
	return encoded.String()
}

func IsPeerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	return st.Code() == codes.Unavailable || st.Code() == codes.DeadlineExceeded ||
		st.Code() == codes.Canceled
}

func SleepRandom(up, down int) {
	// Sleep for a random duration between [down, up] milliseconds
	// This is used to prevent thundering herd problem
	if up > down {
		//nolint:gosec
		sleepTime := down + (up-down)*rand.Intn(1000)/1000
		time.Sleep(time.Duration(sleepTime) * time.Millisecond)
	}

}

func IsBackendUnavailable(err error) bool {
	if err == nil {
		return false
	}
	// Check for common connection closed errors
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		// Check for specific network errors
		if netErr.Err != nil {
			errMsg := netErr.Err.Error()
			return strings.Contains(errMsg, "use of closed network connection") ||
				strings.Contains(errMsg, "connection reset by peer") ||
				strings.Contains(errMsg, "broken pipe") ||
				strings.Contains(errMsg, "connection refused")
		}
		return netErr.Op == "read" || netErr.Op == "write" || netErr.Op == "dial"
	}
	var syscallErr *os.SyscallError
	if errors.As(err, &syscallErr) {
		return errors.Is(syscallErr.Err, syscall.ECONNREFUSED) ||
			errors.Is(syscallErr.Err, syscall.ECONNRESET) ||
			errors.Is(syscallErr.Err, syscall.EPIPE)
	}
	return false
}
