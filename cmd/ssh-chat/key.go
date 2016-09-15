package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/howeyc/gopass"
)

// ReadPrivateKey attempts to read your private key and possibly decrypt it if it
// requires a passphrase.
// This function will prompt for a passphrase on STDIN if the environment variable (`IDENTITY_PASSPHRASE`),
// is not set.
func ReadPrivateKey(path string) ([]byte, error) {
	privateKey, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load identity: %v", err)
	}

	block, rest := pem.Decode(privateKey)
	if len(rest) > 0 {
		return nil, fmt.Errorf("extra data when decoding private key")
	}
	if !x509.IsEncryptedPEMBlock(block) {
		return privateKey, nil
	}

	passphrase := []byte(os.Getenv("IDENTITY_PASSPHRASE"))
	if len(passphrase) == 0 {
		fmt.Print("Enter passphrase: ")
		passphrase, err = gopass.GetPasswd()
		if err != nil {
			return nil, fmt.Errorf("couldn't read passphrase: %v", err)
		}
	}
	der, err := x509.DecryptPEMBlock(block, passphrase)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed: %v", err)
	}

	privateKey = pem.EncodeToMemory(&pem.Block{
		Type:  block.Type,
		Bytes: der,
	})

	return privateKey, nil
}
