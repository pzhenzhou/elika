package respio

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
)

type RespWriter struct {
	writer *bufio.Writer
}

func NewRespWriter(conn net.Conn) *RespWriter {
	return &RespWriter{
		writer: bufio.NewWriterSize(conn, DefaultBufferSize),
	}
}

// WriteStatus writes a status response (e.g., "OK")
func (w *RespWriter) WriteStatus(status string) error {
	if err := w.writer.WriteByte(RespStatus); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(status); err != nil {
		return err
	}
	return w.writeCRLF()
}

func (w *RespWriter) WriteInt64(n int64) error {
	if err := w.writer.WriteByte(RespInt); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.FormatInt(n, 10)); err != nil {
		return err
	}
	return w.writeCRLF()
}

// Write writes a complete RESP packet to the underlying bufio.Writer.
func (w *RespWriter) Write(p *RespPacket) error {
	switch p.Type {
	case RespStatus:
		// +<string>\r\n
		return w.WriteStatus(string(p.Data))

	case RespError:
		// -<string>\r\n
		return w.WriteError(string(p.Data))

	case RespInt:
		// :<int>\r\n
		val, err := strconv.ParseInt(string(p.Data), 10, 64)
		if err != nil {
			return err
		}
		return w.WriteInt64(val)

	case RespString:
		// $<len>\r\n<bytes>\r\n
		return w.WriteBulkString(p.Data)

	case RespArray:
		// *<len>\r\n<element-1>...<element-n>
		return w.WriteArray(p.Array)
	case RespNil:
		// _\r\n
		// Typically just write the Nil type marker:
		if err := w.writer.WriteByte(RespNil); err != nil {
			return err
		}
		return w.writeCRLF()

	case RespFloat:
		// ,<floating>\r\n
		if err := w.writer.WriteByte(RespFloat); err != nil {
			return err
		}
		if _, err := w.writer.WriteString(string(p.Data)); err != nil {
			return err
		}
		return w.writeCRLF()

	case RespBool:
		// #t\r\n or #f\r\n
		if err := w.writer.WriteByte(RespBool); err != nil {
			return err
		}
		b := 'f'
		if string(p.Data) == "t" {
			b = 't'
		}
		if err := w.writer.WriteByte(byte(b)); err != nil {
			return err
		}
		return w.writeCRLF()

	case RespBlobError:
		// !<len>\r\n<bytes>\r\n
		if err := w.writer.WriteByte(RespBlobError); err != nil {
			return err
		}
		return w.WriteBulkString(p.Data)

	case RespVerbatim:
		// =<len>\r\nFORMAT:<bytes>\r\n
		if err := w.writer.WriteByte(RespVerbatim); err != nil {
			return err
		}
		return w.WriteBulkString(p.Data)

	case RespBigInt:
		// (<big int>\r\n
		if err := w.writer.WriteByte(RespBigInt); err != nil {
			return err
		}
		if _, err := w.writer.WriteString(string(p.Data)); err != nil {
			return err
		}
		return w.writeCRLF()

	case RespMap:
		// %<len>\r\n(key)(value)(key)(value)...
		return w.writeArrayLike(RespMap, p.Array, true)

	case RespSet:
		// ~<len>\r\n<element-1>...<element-n>
		return w.writeArrayLike(RespSet, p.Array, false)

	case RespAttr:
		// |<len>\r\n(key)(value)(key)(value)...
		return w.writeArrayLike(RespAttr, p.Array, true)

	case RespPush:
		// ><len>\r\n<element-1>...<element-n>
		return w.writeArrayLike(RespPush, p.Array, false)

	default:
		logger.Info("RespWriter Unknown packet type", "type", p.Type)
		return ErrInvalidSyntax
	}
}

// WriteArray writes an array of RESP packets
func (w *RespWriter) WriteArray(array []*RespPacket) error {
	if array == nil {
		return w.writeNullArray()
	}
	if err := w.writer.WriteByte(RespArray); err != nil {
		logger.Error(err, "RespWriter WriteArray error", "Pkt", array)
		return err
	}
	if _, err := w.writer.WriteString(strconv.Itoa(len(array))); err != nil {
		logger.Error(err, "RespWriter WriteString error", "Pkt", array)
		return err
	}
	if err := w.writeCRLF(); err != nil {
		logger.Error(err, "RespWriter writeCRLF error", "Pkt", array)
		return err
	}
	for _, paket := range array {
		if err := w.Write(paket); err != nil {
			logger.Error(err, "RespWriter Write error", "Pkt", paket)
			return err
		}
	}
	return nil
}

// WriteBulkString writes a bulk string
func (w *RespWriter) WriteBulkString(b []byte) error {
	if b == nil {
		return w.writeNullBulk()
	}
	if err := w.writer.WriteByte(RespString); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.Itoa(len(b))); err != nil {
		return err
	}
	if err := w.writeCRLF(); err != nil {
		return err
	}
	if _, err := w.writer.Write(b); err != nil {
		return err
	}
	return w.writeCRLF()
}

// WriteError writes an error response
func (w *RespWriter) WriteError(err string) error {
	if err := w.writer.WriteByte(RespError); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(err); err != nil {
		return err
	}
	return w.writeCRLF()
}

func (w *RespWriter) writeCRLF() error {
	_, err := w.writer.WriteString(CRLF)
	return err
}

func (w *RespWriter) writeNullBulk() error {
	_, err := w.writer.WriteString(Nil)
	return err
}

func (w *RespWriter) writeNullArray() error {
	_, err := w.writer.WriteString(NilArray)
	return err
}

func (w *RespWriter) writeNullMap() error {
	_, err := w.writer.WriteString("%-1\r\n")
	return err
}

// writeArrayLike writes any array-like type with the given prefix and elements
func (w *RespWriter) writeArrayLike(prefix byte, array []*RespPacket, isMap bool) error {
	if array == nil {
		if prefix == '%' {
			return w.writeNullMap()
		}
		return w.writeNullArray()
	}
	if isMap && len(array)%2 != 0 {
		return fmt.Errorf("invalid map length %d: must contain even number of elements for key-value pairs",
			len(array))
	}

	// Write prefix and length
	if err := w.writer.WriteByte(prefix); err != nil {
		return err
	}

	// For maps, we need to write length/2 since it's key-value pairs
	length := len(array)
	if isMap {
		length = length / 2
	}

	if _, err := w.writer.WriteString(strconv.Itoa(length)); err != nil {
		return err
	}
	if err := w.writeCRLF(); err != nil {
		return err
	}

	// Write elements
	for _, elem := range array {
		// When writing commands to Redis, convert all elements to bulk strings
		if prefix == '*' {
			if err := w.WriteBulkString(elem.Data); err != nil {
				return err
			}
		} else {
			if err := w.Write(elem); err != nil {
				return err
			}
		}
	}
	return nil
}

// Flush writes any buffered data to the underlying io.Writer
func (w *RespWriter) Flush() error {
	return w.writer.Flush()
}
