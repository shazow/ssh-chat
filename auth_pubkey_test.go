package sshchat

import (
	"io"
	"testing"

	"github.com/shazow/ssh-chat/sshd"
)

func TestAuthRequirePubkey(t *testing.T) {
	key, err := NewRandomPublicKey(2048)
	if err != nil {
		t.Fatal(err)
	}

	auth := NewAuth()
	if !auth.AllowAnonymous() {
		t.Error("anonymous access should be allowed by default")
	}
	if auth.RequirePubkey() {
		t.Error("RequirePubkey should be off by default")
	}

	auth.SetRequirePubkey(true)
	if auth.AllowAnonymous() {
		t.Error("anonymous access should be refused when RequirePubkey is set")
	}
	// Any key is still accepted.
	if err := auth.CheckPublicKey(key); err != nil {
		t.Errorf("a public key should be accepted: %v", err)
	}

	// An allowlist still takes precedence over the any-key behaviour.
	auth.SetAllowlistMode(true)
	if err := auth.CheckPublicKey(key); err != ErrNotAllowed {
		t.Errorf("expected ErrNotAllowed for a key outside the allowlist, got %v", err)
	}
}

func TestHostRequirePubkey(t *testing.T) {
	noop := func(r io.Reader, w io.WriteCloser) error { return nil }

	signer, err := sshd.NewRandomSigner(2048)
	if err != nil {
		t.Fatal(err)
	}

	auth := NewAuth()
	s, host := getHost(t, auth)
	defer s.Close()
	go host.Serve()
	target := s.Addr().String()

	// Default: clients without a public key are accepted.
	if err := sshd.ConnectShell(target, "foo", noop); err != nil {
		t.Errorf("keyless connection should be accepted by default: %v", err)
	}

	auth.SetRequirePubkey(true)

	// With the option set: no key, no entry.
	if err := sshd.ConnectShell(target, "foo", noop); err == nil {
		t.Error("keyless connection should be refused with RequirePubkey")
	}
	// Any public key is still accepted.
	if err := sshd.ConnectShellWithKey(target, "foo", signer, noop); err != nil {
		t.Errorf("connection with a public key should be accepted: %v", err)
	}
}
