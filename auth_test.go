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

func TestAuthWhitelist(t *testing.T) {
	key, err := NewRandomPublicKey(512)
	if err != nil {
		t.Fatal(err)
	}

	auth := NewAuth()
	err = auth.Check(nil, key, "")
	if err != nil {
		t.Error("Failed to permit in default state:", err)
	}

	auth.Whitelist(key, 0)

	keyClone, err := ClonePublicKey(key)
	if err != nil {
		t.Fatal(err)
	}

	if string(keyClone.Marshal()) != string(key.Marshal()) {
		t.Error("Clone key does not match.")
	}

	err = auth.Check(nil, keyClone, "")
	if err != nil {
		t.Error("Failed to permit whitelisted:", err)
	}

	key2, err := NewRandomPublicKey(512)
	if err != nil {
		t.Fatal(err)
	}

	err = auth.Check(nil, key2, "")
	if err == nil {
		t.Error("Failed to restrict not whitelisted:", err)
	}
}

func TestAuthPasswords(t *testing.T) {
	auth := NewAuth()

	if auth.AcceptPassword() {
		t.Error("Doesn't known it won't accept passwords.")
	}
	auth.SetPassword("")
	if auth.AcceptPassword() {
		t.Error("Doesn't known it won't accept passwords.")
	}

	err := auth.CheckPassword("Pa$$w0rd")
	if err == nil {
		t.Error("Failed to deny without password:", err)
	}

	auth.SetPassword("Pa$$w0rd")

	err = auth.CheckPassword("Pa$$w0rd")
	if err != nil {
		t.Error("Failed to allow vaild password:", err)
	}

	err = auth.CheckPassword("something else")
	if err == nil {
		t.Error("Failed to restrict wrong password:", err)
	}

	auth.SetPassword("")
	if auth.AcceptPassword() {
		t.Error("Didn't clear password.")
	}
}
