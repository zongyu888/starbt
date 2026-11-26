//go:build !linux
// +build !linux

package ssserver

import "errors"

func Serve(listenAddr, password, method string) error {
	return errors.New("sbt Shadowsocks 服务端仅支持 Linux")
}
