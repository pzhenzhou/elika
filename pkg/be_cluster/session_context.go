package be_cluster

import (
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/pzhenzhou/elika/pkg/respio"
)

type RequestContext struct {
	Request  *respio.RespPacket
	Session  *Session
	AuthInfo *common.AuthInfo
}

type ResponseContext struct {
	Response *respio.RespPacket
	Callback func(*Session)
}

func NewErrResponseContext(err error) *ResponseContext {
	return &ResponseContext{
		Response: &respio.RespPacket{
			Type: respio.RespError,
			Data: []byte(err.Error()),
		},
	}
}
