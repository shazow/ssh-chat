package sshd

import (
	"io"
	"net"

	"github.com/shazow/rateio"
)

type limitedConn struct {
	net.Conn
	io.Reader // Our rate-limited io.Reader for net.Conn
}

func (r *limitedConn) Read(p []byte) (n int, err error) {
	return r.Reader.Read(p)
}

// ReadLimitConn returns a net.Conn whose io.Reader interface is rate-limited by limiter.
func ReadLimitConn(conn net.Conn, limiter rateio.Limiter) net.Conn {
	return &limitedConn{
		Conn:   conn,
		Reader: rateio.NewReader(conn, limiter),
	}
}
