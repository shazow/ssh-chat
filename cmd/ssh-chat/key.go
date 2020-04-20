package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/howeyc/gopass"
	"golang.org/x/crypto/ssh"
)

// ReadPrivateKey attempts to read your private key and possibly decrypt it if it
// requires a passphrase.
// This function will prompt for a passphrase on STDIN if the environment variable (`IDENTITY_PASSPHRASE`),
// is not set.
func ReadPrivateKey(path string) (ssh.Signer, error) {
	privateKey, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load identity: %v", err)
	}

	pk, err := ssh.ParsePrivateKey(privateKey)
	if err == nil {
	} else if _, ok := err.(*ssh.PassphraseMissingError); ok {
		passphrase := []byte(os.Getenv("IDENTITY_PASSPHRASE"))
		if len(passphrase) == 0 {
			fmt.Print("Enter passphrase: ")
			passphrase, err = gopass.GetPasswd()
			if err != nil {
				return nil, fmt.Errorf("couldn't read passphrase: %v", err)
			}
		}
		return ssh.ParsePrivateKeyWithPassphrase(privateKey, passphrase)
	}

	return pk, err
}
