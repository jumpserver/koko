/*
 * Copyright (c) 2000-2018, 达梦数据库有限公司.
 * All rights reserved.
 */

package security

import (
	"crypto/tls"
	"errors"
	"net"
	"sync"
)

// var dmHome = flag.String("DM_HOME", "", "Where DMDB installed")
var flagLock = sync.Mutex{}

func NewTLSFromTCP(conn net.Conn, sslCertPath string, sslKeyPath string, user string, authOnly bool) (*tls.Conn, error) {
	var conf *tls.Config
	if !authOnly {
		conf = &tls.Config{
			InsecureSkipVerify: true, //跳过证书校验
		}
	} else {
		if sslCertPath == "" && sslKeyPath == "" {
			// 用户必须手动指定ssl文件和签名(.cert文件)
			return nil, errors.New("sslCertPath and sslKeyPath can not be empty!")

		}
		cer, err := tls.LoadX509KeyPair(sslCertPath, sslKeyPath)
		if err != nil {
			return nil, err
		}
		conf = &tls.Config{
			InsecureSkipVerify: true,
			Certificates:       []tls.Certificate{cer},
		}
	}
	tlsConn := tls.Client(conn, conf)
	if err := tlsConn.Handshake(); err != nil {
		return nil, err
	}
	return tlsConn, nil
}
