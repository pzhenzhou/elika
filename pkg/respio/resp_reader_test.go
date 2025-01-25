package respio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// RespTestCase defines the structure for RESP protocol test cases
type RespTestCase struct {
	name     string
	input    []byte
	expected []*RespPacket
}

func TestRespReader_Read(t *testing.T) {
	// redis-cli> HSET myhash field1 "Hello"
	// redis-cli> HSET myhash field2 "World"
	// redis-cli> HMGET myhash field1 field2 nofield
	// 1) "Hello"
	// 2) "World"
	// 3) (nil)
	hmsetCmd := [][]byte{
		[]byte("*4\r\n$4\r\nHSET\r\n$6\r\nmyhash\r\n$6\r\nfield1\r\n$5\r\nHello\r\n"),
		[]byte("*4\r\n$4\r\nHSET\r\n$6\r\nmyhash\r\n$6\r\nfield2\r\n$5\r\nWorld\r\n"),
		[]byte("*5\r\n$5\r\nHMGET\r\n$6\r\nmyhash\r\n$6\r\nfield1\r\n$6\r\nfield2\r\n$7\r\nnofield\r\n"),
	}

	tests := []RespTestCase{
		{
			name:  "HSET field1",
			input: hmsetCmd[0],
			expected: []*RespPacket{
				{
					Type: RespArray,
					Array: []*RespPacket{
						{Type: RespString, Data: []byte("HSET")},
						{Type: RespString, Data: []byte("myhash")},
						{Type: RespString, Data: []byte("field1")},
						{Type: RespString, Data: []byte("Hello")},
					},
				},
			},
		},
		{
			name:  "HSET field2",
			input: hmsetCmd[1],
			expected: []*RespPacket{
				{
					Type: RespArray,
					Array: []*RespPacket{
						{Type: RespString, Data: []byte("HSET")},
						{Type: RespString, Data: []byte("myhash")},
						{Type: RespString, Data: []byte("field2")},
						{Type: RespString, Data: []byte("World")},
					},
				},
			},
		},
		{
			name:  "HMGET fields",
			input: hmsetCmd[2],
			expected: []*RespPacket{
				{
					Type: RespArray,
					Array: []*RespPacket{
						{Type: RespString, Data: []byte("HMGET")},
						{Type: RespString, Data: []byte("myhash")},
						{Type: RespString, Data: []byte("field1")},
						{Type: RespString, Data: []byte("field2")},
						{Type: RespString, Data: []byte("nofield")},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewRespReaderFromBytes(tt.input)
			for _, expected := range tt.expected {
				result, err := reader.Read()
				assert.NoError(t, err)
				assert.Equal(t, expected.Type, result.Type)
				assert.Equal(t, len(expected.Array), len(result.Array))

				for i, expectedElem := range expected.Array {
					assert.Equal(t, expectedElem.Type, result.Array[i].Type)
					assert.Equal(t, expectedElem.Data, result.Array[i].Data)
				}
			}
		})
	}
}
