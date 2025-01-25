package common

import (
	"bytes"
	"strconv"
	"sync/atomic"
)

type AuthInfo struct {
	Username   []byte `json:"username,omitempty"`
	Password   []byte `json:"password,omitempty"`
	TenantCode uint64
}

func (a *AuthInfo) Equals(b *AuthInfo) bool {
	return bytes.Equal(a.Username, b.Username) && bytes.Equal(a.Password, b.Password)
}

func (a *AuthInfo) ToString() string {
	return "Username: " + string(a.Username) + ", Password: " + string(a.Password) + ", TenantCode: " + strconv.FormatUint(a.TenantCode, 10)
}

func (a *AuthInfo) LoadTenantCode() uint64 {
	return atomic.LoadUint64(&a.TenantCode)
}
