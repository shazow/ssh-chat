package sshd

import (
	"errors"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

var errRejectAuth = errors.New("not welcome here")

type RejectAuth struct{}

func (a RejectAuth) AllowAnonymous() bool {
	return false
}
func (a RejectAuth) AcceptPassphrase() bool {
	return false
}
func (a RejectAuth) CheckBans(addr net.Addr, key ssh.PublicKey, clientVersion string) error {
	return errRejectAuth
}
func (a RejectAuth) CheckPubkey(ssh.PublicKey) error {
	return errRejectAuth
}
func (a RejectAuth) CheckPassphrase(string) error {
	return errRejectAuth
}
func (a RejectAuth) BanAddr(net.Addr, time.Duration) {}

func TestClientReject(t *testing.T) {
	signer, err := NewRandomSigner(512)
	if err != nil {
		t.Fatal(err)
	}
	config := MakeAuth(RejectAuth{})
	config.AddHostKey(signer)

	s, err := ListenSSH("localhost:0", config)
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
}
