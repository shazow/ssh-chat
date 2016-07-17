package sshd

import (
	"errors"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"
)

var errRejectAuth = errors.New("not welcome here")

type RejectAuth struct{}

func (a RejectAuth) AllowAnonymous() bool {
	return false
}
func (a RejectAuth) Check(net.Addr, ssh.PublicKey) (bool, error) {
	return false, errRejectAuth
}

func TestClientReject(t *testing.T) {
	signer, err := NewRandomSigner(512)
	config := MakeAuth(RejectAuth{})
	config.AddHostKey(signer)

	s, err := ListenSSH(":0", config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	go s.Serve()

	conn, err := ssh.Dial("tcp", s.Addr().String(), NewClientConfig("foo"))
	if err == nil {
		defer conn.Close()
		t.Error("Failed to reject conncetion")
	}
	t.Log(err)
}
