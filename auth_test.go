package sshchat

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"golang.org/x/crypto/ssh"
)

func NewRandomPublicKey(bits int) (ssh.PublicKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, err
	}

	return ssh.NewPublicKey(key.Public())
}

func ClonePublicKey(key ssh.PublicKey) (ssh.PublicKey, error) {
	return ssh.ParsePublicKey(key.Marshal())
}

func TestAuthAllowlist(t *testing.T) {
	key, err := NewRandomPublicKey(512)
	if err != nil {
		t.Fatal(err)
	}

	auth := NewAuth()
	err = auth.CheckPublicKey(key)
	if err != nil {
		t.Error("Failed to permit in default state:", err)
	}

	auth.Allowlist(key, 0)
	auth.SetAllowlistMode(true)

	keyClone, err := ClonePublicKey(key)
	if err != nil {
		t.Fatal(err)
	}

	if string(keyClone.Marshal()) != string(key.Marshal()) {
		t.Error("Clone key does not match.")
	}

	err = auth.CheckPublicKey(keyClone)
	if err != nil {
		t.Error("Failed to permit allowlisted:", err)
	}

	key2, err := NewRandomPublicKey(512)
	if err != nil {
		t.Fatal(err)
	}

	err = auth.CheckPublicKey(key2)
	if err == nil {
		t.Error("Failed to restrict not allowlisted:", err)
	}
}

func TestAuthPassphrases(t *testing.T) {
	auth := NewAuth()

	if auth.AcceptPassphrase() {
		t.Error("Doesn't known it won't accept passphrases.")
	}
	auth.SetPassphrase("")
	if auth.AcceptPassphrase() {
		t.Error("Doesn't known it won't accept passphrases.")
	}

	err := auth.CheckPassphrase("Pa$$w0rd")
	if err == nil {
		t.Error("Failed to deny without passphrase:", err)
	}

	auth.SetPassphrase("Pa$$w0rd")

	err = auth.CheckPassphrase("Pa$$w0rd")
	if err != nil {
		t.Error("Failed to allow vaild passphrase:", err)
	}

	err = auth.CheckPassphrase("something else")
	if err == nil {
		t.Error("Failed to restrict wrong passphrase:", err)
	}

	auth.SetPassphrase("")
	if auth.AcceptPassphrase() {
		t.Error("Didn't clear passphrase.")
	}
}
