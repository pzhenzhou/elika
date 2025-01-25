package respio

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"github.com/pzhenzhou/elika/pkg/common"
	"io"
	"net"
	"strconv"
)

const (
	DefaultBufferSize = 8 * common.KB // 8KB
	MaxBufferSize     = 512 * common.MB
)

var (
	ErrInvalidSyntax = errors.New("invalid RESP syntax")
	ErrTooLarge      = errors.New("value too large")
	ErrBadCRLFEnd    = errors.New("bad CRLF end")
)

type RespReader struct {
	reader *bufio.Reader
}

func NewRespReader(conn net.Conn) *RespReader {
	return &RespReader{
		reader: bufio.NewReaderSize(conn, DefaultBufferSize),
	}
}

func NewRespReaderFromBytes(data []byte) *RespReader {
	return &RespReader{
		reader: bufio.NewReader(bytes.NewReader(data)),
	}
}

// Read reads a complete RESP message and returns it as a RespPacket
func (r *RespReader) Read() (*RespPacket, error) {
	b, err := r.reader.ReadByte()
	if err != nil {
		// logger.Error(err, "RespReader Failed to read byte")
		return nil, err
	}

	switch b {
	case RespStatus: // Simple String
		data, err := r.readLine()
		if err != nil {
			return nil, err
		}
		return &RespPacket{Type: RespStatus, Data: data}, nil

	case RespError: // Error
		data, err := r.readLine()
		if err != nil {
			return nil, err
		}
		return &RespPacket{Type: RespError, Data: data}, nil

	case RespInt: // Integer
		if err := r.reader.UnreadByte(); err != nil {
			return nil, err
		}
		n, err := r.ReadInt()
		if err != nil {
			logger.Error(err, "RespInt Failed to read int")
			return nil, err
		}
		return &RespPacket{Type: RespInt, Data: []byte(strconv.FormatInt(n, 10))}, nil

	case RespString: // Bulk String
		if err := r.reader.UnreadByte(); err != nil {
			return nil, err
		}
		data, err := r.BulkReadStringWithMarker(RespString)
		if err != nil {
			return nil, err
		}
		return &RespPacket{Type: RespString, Data: data}, nil

	case RespNil:
		// RESP3 Null
		// `_` means a Null value
		// read CRLF and return a nil value
		if err := r.skipCRLF(); err != nil {
			return nil, err
		}
		return NilPacket, nil
	case RespBool:
		boolVal, err := r.ReadBool()
		if err != nil {
			return nil, err
		}
		return &RespPacket{Type: RespBool, Data: []byte(strconv.FormatBool(boolVal))}, nil
	case RespFloat:
		// RESP3 Double/Float
		floatVal, err := r.ReadFloat()
		if err != nil {
			return nil, err
		}
		return &RespPacket{Type: RespFloat, Data: []byte(fmt.Sprintf("%g", floatVal))}, nil
	case RespBigInt: // '('
		// RESP3 Big integer
		bigIntVal, err := r.ReadBigInt()
		if err != nil {
			return nil, err
		}
		return &RespPacket{Type: RespBigInt, Data: []byte(bigIntVal)}, nil
	case RespVerbatim: // '='
		// RESP3 Verbatim string
		//	This is basically a bulk string with a "FORMAT:" prefix. For example:
		//	  =15\r\ntxt:Some string\r\n
		//	The first line after '=' is the length, same as a bulk string length
		//	Then read that many bytes + CRLF, etc.
		//	Unread the '=' so we can re-use your BulkReadString logic
		//	(like we do with '$') or write a new function:
		verbatimVal, err := r.ReadTypesWithMarker(RespVerbatim)
		if err != nil {
			return nil, err
		}
		return &RespPacket{Type: RespVerbatim, Data: verbatimVal}, nil
	case RespBlobError: // '!'
		// RESP3 Blob error
		blobErr, err := r.ReadTypesWithMarker(RespBlobError)
		if err != nil {
			return nil, err
		}
		return &RespPacket{Type: RespBlobError, Data: blobErr}, nil
	case RespArray: // Array
		return r.ReadArrayLike(b, RespArray, 1)
	case RespPush: // '>'
		// RESP3 Push data
		return r.ReadArrayLike(b, RespPush, 1)
	case RespMap: // '%'
		// RESP3 Map
		// read a map length, then read key/value pairs
		return r.ReadArrayLike(b, RespMap, 2)
	case RespSet: // '~'
		// RESP3 Set
		return r.ReadArrayLike(b, RespSet, 1)
	case RespAttr: // '|'
		return r.ReadArrayLike(b, RespAttr, 2)
	default:
		logger.Info("RespReader Invalid RESP type", "type", string(b))
		return nil, ErrInvalidSyntax
	}
}

func (r *RespReader) Buffered() int {
	return r.reader.Buffered()
}

// ReadInt reads an integer from a RESP stream
func (r *RespReader) ReadInt() (int64, error) {
	// Read until newline in one operation
	b, err := r.reader.ReadSlice('\n')
	if err != nil {
		return 0, err
	}

	// Check for proper CRLF ending
	if n := len(b) - 2; n < 0 || b[n] != '\r' {
		return 0, ErrBadCRLFEnd
	}

	// Skip the type marker if present (:, $, *)
	start := 0
	if len(b) > 0 && (b[0] == ':' || b[0] == '$' || b[0] == '*') {
		start = 1
	}

	// Parse the number (excluding CRLF)
	return encodeToInt64(b[start : len(b)-2])
}

// Helper function for parsing integers
func encodeToInt64(b []byte) (int64, error) {
	if len(b) == 0 {
		return 0, ErrInvalidSyntax
	}
	if len(b) < 10 { // Fast path for small numbers
		var neg, i = false, 0
		switch b[0] {
		case '-':
			neg = true
			fallthrough
		case '+':
			i++
		}
		if len(b) != i {
			var n int64
			for ; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
				n = int64(b[i]-'0') + n*10
			}
			if len(b) == i {
				if neg {
					n = -n
				}
				return n, nil
			}
		}
	}
	return strconv.ParseInt(string(b), 10, 64)
}

func (r *RespReader) ReadBigInt() (string, error) {
	line, err := r.readLine()
	if err != nil {
		return "", err
	}
	// You could parse it as a big.Int if you want, or just store as string.
	return string(line), nil
}

func (r *RespReader) ReadTypesWithMarker(marker byte) ([]byte, error) {
	// This is a bit like a bulk string, but the data is a type name.
	if err := r.reader.UnreadByte(); err != nil {
		return nil, err
	}
	data, err := r.BulkReadStringWithMarker(marker)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (r *RespReader) ReadResp3Len(expectedMarker byte) (int, error) {
	if err := r.reader.UnreadByte(); err != nil {
		return 0, err
	}
	// This uses the same approach as your array reading:
	b, err := r.reader.ReadByte()
	if err != nil {
		return 0, err
	}
	if b != expectedMarker {
		return 0, ErrInvalidSyntax
	}

	length, err := r.ReadInt()
	if err != nil {
		return 0, err
	}

	if length > 1024*1024 {
		return 0, ErrTooLarge
	}

	return int(length), nil
}

// BulkReadStringWithMarker Reads a bulk string with a specific marker
func (r *RespReader) BulkReadStringWithMarker(marker byte) ([]byte, error) {
	b, err := r.reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if b != marker {
		return nil, ErrInvalidSyntax
	}
	length, err := r.ReadInt()
	if err != nil {
		return nil, err
	}
	if length == -1 {
		return nil, nil
	}
	if length > MaxBufferSize {
		return nil, ErrTooLarge
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r.reader, buf); err != nil {
		return nil, err
	}

	if err := r.skipCRLF(); err != nil {
		return nil, err
	}
	return buf, nil
}

func (r *RespReader) ReadFloat() (float64, error) {
	// After ',' marker, we read until CRLF.
	line, err := r.readLine()
	if err != nil {
		return 0, err
	}
	// Convert to float64
	f, err := strconv.ParseFloat(string(line), 64)
	if err != nil {
		return 0, err
	}
	return f, nil
}

func (r *RespReader) ReadBool() (bool, error) {
	// After reading '#' from the stream, we expect either 't\r\n' or 'f\r\n'.
	b, err := r.reader.ReadByte()
	if err != nil {
		return false, err
	}
	var val bool
	switch b {
	case 't':
		val = true
	case 'f':
		val = false
	default:
		return false, ErrInvalidSyntax
	}
	// Next should cluster CRLF
	if err := r.skipCRLF(); err != nil {
		return false, err
	}
	return val, nil
}

func (r *RespReader) readLine() ([]byte, error) {
	// ReadSlice stops after finding '\n' but returns the entire slice including that '\n'.
	line, err := r.reader.ReadSlice('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, ErrBadCRLFEnd
	}
	// Chop off the trailing "\r\n" so what we return is just the line data.
	return line[:len(line)-2], nil
}

// skipCRLF reads and validates CRLF
func (r *RespReader) skipCRLF() error {
	b, err := r.reader.ReadByte()
	if err != nil {
		return err
	}
	if b != '\r' {
		return ErrInvalidSyntax
	}

	b, err = r.reader.ReadByte()
	if err != nil {
		return err
	}
	if b != '\n' {
		return ErrInvalidSyntax
	}
	return nil
}

func (r *RespReader) ReadArrayLike(marker, respType byte, multiplier int) (*RespPacket, error) {
	length, err := r.ReadResp3Len(marker)
	if err != nil {
		return nil, err
	}
	if length == -1 {
		return &RespPacket{Type: respType}, nil
	}
	// For maps and attributes, we need twice as many elements (key-value pairs)
	numElements := length * multiplier
	items := make([]*RespPacket, numElements)
	for i := 0; i < numElements; i++ {
		elem, err := r.Read()
		if err != nil {
			return nil, err
		}
		items[i] = elem
	}
	return &RespPacket{Type: respType, Array: items}, nil
}
