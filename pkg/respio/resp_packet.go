package respio

import (
	"bytes"
	"fmt"
	"github.com/pzhenzhou/elika/pkg/common"
	"strings"
)

var (
	logger = common.InitLogger().WithName("resp")

	NilPacket = &RespPacket{Type: RespNil}

	ErrNoAuth = &RespPacket{
		Type: RespError,
		Data: []byte("NOAUTH Authentication required"),
	}

	ErrAuthFailed = &RespPacket{
		Type: RespError,
		Data: []byte("WRONGPASS invalid username-password pair or user is disabled"),
	}

	ErrBackendUnavailable = &RespPacket{
		Type: RespError,
		Data: []byte("ERR backend is unavailable"),
	}

	ErrGetConnFromPool = &RespPacket{
		Type: RespError,
		Data: []byte("ERR can not get backend connection from pool"),
	}

	OkStatus = &RespPacket{Type: RespStatus, Data: []byte("OK")}
)

type RespPacket struct {
	Type  byte
	Data  []byte
	Array []*RespPacket
}

func (p *RespPacket) GetCommand() []byte {
	if p.Type == RespArray && len(p.Array) > 0 {
		return p.Array[0].Data
	}
	return p.Data
}

func (p *RespPacket) IsAuthCmd() bool {
	if p.Type != RespArray || len(p.Array) < 2 {
		return false
	}
	cmdPkt := p.Array[0]
	if cmdPkt.Type != RespString {
		return false
	}
	return bytes.EqualFold(cmdPkt.Data, AuthCmd)
}

// String returns a string representation of the RespPacket
// Only for debugging purposes
func (p *RespPacket) String() string {
	switch p.Type {
	case RespStatus:
		return fmt.Sprintf("Status: \"%s\"", string(p.Data))

	case RespError:
		return fmt.Sprintf("Error: %s", string(p.Data))

	case RespInt:
		return fmt.Sprintf("Integer: %s", string(p.Data))

	case RespString:
		if p.Data == nil {
			return "String: (nil)"
		}
		return fmt.Sprintf("String: \"%s\"", string(p.Data))

	case RespArray:
		if p.Array == nil {
			return "Array: (nil)"
		}
		if len(p.Array) == 0 {
			return "Array: (empty)"
		}

		var b strings.Builder
		b.WriteString("Array:\n")
		for i, elem := range p.Array {
			elemStr := elem.String()
			lines := strings.Split(elemStr, "\n")
			b.WriteString(fmt.Sprintf("  %d) %s\n", i+1, lines[0]))
			for _, line := range lines[1:] {
				b.WriteString(fmt.Sprintf("     %s\n", line))
			}
		}
		return strings.TrimRight(b.String(), "\n")

	case RespNil:
		return "(nil)"

	case RespFloat:
		return fmt.Sprintf("Float: %s", string(p.Data))

	case RespBool:
		return fmt.Sprintf("Bool: %s", string(p.Data))

	case RespBlobError:
		return fmt.Sprintf("BlobError: %s", string(p.Data))

	case RespVerbatim:
		return fmt.Sprintf("Verbatim: %s", string(p.Data))

	case RespBigInt:
		return fmt.Sprintf("BigInt: %s", string(p.Data))

	case RespMap:
		if p.Array == nil {
			return "Map: (nil)"
		}
		var b strings.Builder
		b.WriteString("Map:\n")
		for i := 0; i < len(p.Array); i += 2 {
			key := p.Array[i].String()
			value := "nil"
			if i+1 < len(p.Array) {
				value = p.Array[i+1].String()
			}
			b.WriteString(fmt.Sprintf("  %s => %s\n", key, value))
		}
		return strings.TrimRight(b.String(), "\n")

	case RespSet:
		return fmt.Sprintf("Set:%s", p.Array) // Similar to Array

	case RespAttr:
		return fmt.Sprintf("Attr:%s", p.Array) // Similar to Map

	case RespPush:
		return fmt.Sprintf("Push:%s", p.Array) // Similar to Array

	default:
		return fmt.Sprintf("(unknown type: %c)", p.Type)
	}
}

func (p *RespPacket) ToAuthInfo() *common.AuthInfo {
	if !p.IsAuthCmd() {
		return nil
	}
	authData := p.Array
	if len(authData) != 3 {
		return &common.AuthInfo{
			// Username: []byte("default"),
			Password: authData[1].Data,
		}
	} else {
		authUser := authData[1].Data
		idx := bytes.IndexByte(authUser, common.TenantKeySeparator)
		var tenantKey []byte
		username := authUser
		if idx != -1 {
			tenantKey = authUser[:idx]
			username = authUser[idx+1:]
		}
		tenantCode, _ := common.DecodeBase62(string(tenantKey))
		auth := &common.AuthInfo{
			Username:   username,
			Password:   authData[2].Data,
			TenantCode: tenantCode,
		}
		return auth
	}
}

func (p *RespPacket) IsTxCmd() ([]byte, TxCmdStateType, bool) {
	cmd := p.GetCommand()
	if bytes.EqualFold(cmd, MultiCmd) || bytes.EqualFold(cmd, WatchCmd) {
		return cmd, TxCmdStateBegin, true
	} else if bytes.EqualFold(cmd, ExecCmd) || bytes.EqualFold(cmd, DiscardCmd) {
		return cmd, TxCmdStateEnd, true
	} else {
		return cmd, "", false
	}
}

func NewAuthPacket(username, password []byte) *RespPacket {
	if username == nil {
		return &RespPacket{
			Type: RespArray,
			Array: []*RespPacket{
				{Type: RespString, Data: AuthCmd},
				{Type: RespString, Data: password},
			},
		}
	}
	return &RespPacket{
		Type: RespArray,
		Array: []*RespPacket{
			{Type: RespString, Data: AuthCmd},
			{Type: RespString, Data: username},
			{Type: RespString, Data: password},
		},
	}
}
