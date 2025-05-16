package common

import (
	"bytes"
)

type AuthInfo struct {
	Username []byte `json:"username,omitempty"`
	Password []byte `json:"password,omitempty"`
}

func (a *AuthInfo) Equals(b *AuthInfo) bool {
	return bytes.Equal(a.Username, b.Username) && bytes.Equal(a.Password, b.Password)
}

func (a *AuthInfo) ToString() string {
	return "Username: " + string(a.Username) + ", Password: " + string(a.Password)
}
