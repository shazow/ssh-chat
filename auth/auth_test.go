package auth

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
	ok, err := auth.Check(nil, key)
	if !ok || err != nil {
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

	ok, err = auth.Check(nil, keyClone)
	if !ok || err != nil {
		t.Error("Failed to permit whitelisted:", err)
	}

	key2, err := NewRandomPublicKey(512)
	if err != nil {
		t.Fatal(err)
	}

	ok, err = auth.Check(nil, key2)
	if ok || err == nil {
		t.Error("Failed to restrict not whitelisted:", err)
	}

}
