package main

import (
	"bufio"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strings"

	"github.com/alexcesaro/log"
	"github.com/alexcesaro/log/golog"
	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/sshd"
)
import _ "net/http/pprof"

// Options contains the flag options
type Options struct {
	Verbose   []bool   `short:"v" long:"verbose" description:"Show verbose logging."`
	Identity  string   `short:"i" long:"identity" description:"Private key to identify server with." default:"~/.ssh/id_rsa"`
	Bind      string   `long:"bind" description:"Host and port to listen on." default:"0.0.0.0:22"`
	Admin     []string `long:"admin" description:"Fingerprint of pubkey to mark as admin."`
	Whitelist string   `long:"whitelist" description:"Optional file of pubkey fingerprints who are allowed to connect."`
	Motd      string   `long:"motd" description:"Optional Message of the Day file."`
	Pprof     int      `long:"pprof" description:"Enable pprof http server for profiling."`
}

var logLevels = []log.Level{
	log.Warning,
	log.Info,
	log.Debug,
}

var buildCommit string

func main() {
	options := Options{}
	parser := flags.NewParser(&options, flags.Default)
	p, err := parser.Parse()
	if err != nil {
		if p == nil {
			fmt.Print(err)
		}
		os.Exit(1)
		return
	}

	if options.Pprof != 0 {
		go func() {
			fmt.Println(http.ListenAndServe(fmt.Sprintf("localhost:%d", options.Pprof), nil))
		}()
	}

	// Figure out the log level
	numVerbose := len(options.Verbose)
	if numVerbose > len(logLevels) {
		numVerbose = len(logLevels)
	}

	logLevel := logLevels[numVerbose]
	logger = golog.New(os.Stderr, logLevel)

	if logLevel == log.Debug {
		// Enable logging from submodules
		chat.SetLogger(os.Stderr)
		sshd.SetLogger(os.Stderr)
	}

	privateKeyPath := options.Identity
	if strings.HasPrefix(privateKeyPath, "~/") {
		user, err := user.Current()
		if err == nil {
			privateKeyPath = strings.Replace(privateKeyPath, "~", user.HomeDir, 1)
		}
	}

	privateKey, err := readPrivateKey(privateKeyPath)
	if err != nil {
		logger.Errorf("Couldn't read private key: %v", err)
		os.Exit(2)
	}

	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		logger.Errorf("Failed to parse key: %v", err)
		os.Exit(3)
	}

	auth := NewAuth()
	config := sshd.MakeAuth(auth)
	config.AddHostKey(signer)

	s, err := sshd.ListenSSH(options.Bind, config)
	if err != nil {
		logger.Errorf("Failed to listen on socket: %v", err)
		os.Exit(4)
	}
	defer s.Close()

	host := NewHost(s)
	host.auth = &auth

	for _, fingerprint := range options.Admin {
		auth.Op(fingerprint)
	}

	if options.Whitelist != "" {
		file, err := os.Open(options.Whitelist)
		if err != nil {
			logger.Errorf("Could not open whitelist file")
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			auth.Whitelist(scanner.Text())
		}
	}

	if options.Motd != "" {
		motd, err := ioutil.ReadFile(options.Motd)
		if err != nil {
			logger.Errorf("Failed to load MOTD file: %v", err)
			return
		}
		motdString := string(motd[:])
		// hack to normalize line endings into \r\n
		motdString = strings.Replace(motdString, "\r\n", "\n", -1)
		motdString = strings.Replace(motdString, "\n", "\r\n", -1)
		host.SetMotd(motdString)
	}

	go host.Serve()

	// Construct interrupt handler
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	<-sig // Wait for ^C signal
	logger.Warningf("Interrupt signal detected, shutting down.")
	os.Exit(0)
}

// readPrivateKey attempts to read your private key and possibly decrypt it if it
// requires a passphrase.
// This function will prompt for a passphrase on STDIN if the environment variable (`IDENTITY_PASSPHRASE`),
// is not set.
func readPrivateKey(privateKeyPath string) ([]byte, error) {
	privateKey, err := ioutil.ReadFile(privateKeyPath)
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
		fmt.Printf("Enter passphrase: ")
		passphrase, err = terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return nil, fmt.Errorf("couldn't read passphrase: %v", err)
		}
		fmt.Println()
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
