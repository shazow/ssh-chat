package main

import (
	"bufio"
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

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)
import _ "net/http/pprof"

// Options contains the flag options
type Options struct {
	Verbose   []bool `short:"v" long:"verbose" description:"Show verbose logging."`
	Identity  string `short:"i" long:"identity" description:"Private key to identify server with." default:"~/.ssh/id_rsa"`
	Bind      string `long:"bind" description:"Host and port to listen on." default:"0.0.0.0:2022"`
	Admin     string `long:"admin" description:"File of public keys who are admins."`
	Whitelist string `long:"whitelist" description:"Optional file of public keys who are allowed to connect."`
	Motd      string `long:"motd" description:"Optional Message of the Day file."`
	Log       string `long:"log" description:"Write chat log to this file."`
	Pprof     int    `long:"pprof" description:"Enable pprof http server for profiling."`
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
		numVerbose = len(logLevels) - 1
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

	privateKey, err := ReadPrivateKey(privateKeyPath)
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
	s.RateLimit = true

	fmt.Printf("Listening for connections on %v\n", s.Addr().String())

	host := NewHost(s)
	host.auth = auth
	host.theme = &message.Themes[0]

	err = fromFile(options.Admin, func(line []byte) error {
		key, _, _, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			return err
		}
		auth.Op(key, 0)
		return nil
	})
	if err != nil {
		logger.Errorf("Failed to load admins: %v", err)
		os.Exit(5)
	}

	err = fromFile(options.Whitelist, func(line []byte) error {
		key, _, _, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			return err
		}
		auth.Whitelist(key, 0)
		logger.Debugf("Whitelisted: %s", line)
		return nil
	})
	if err != nil {
		logger.Errorf("Failed to load whitelist: %v", err)
		os.Exit(5)
	}

	if options.Motd != "" {
		motd, err := ioutil.ReadFile(options.Motd)
		if err != nil {
			logger.Errorf("Failed to load MOTD file: %v", err)
			return
		}
		motdString := strings.TrimSpace(string(motd))
		// hack to normalize line endings into \r\n
		motdString = strings.Replace(motdString, "\r\n", "\n", -1)
		motdString = strings.Replace(motdString, "\n", "\r\n", -1)
		host.SetMotd(motdString)
	}

	if options.Log == "-" {
		host.SetLogging(os.Stdout)
	} else if options.Log != "" {
		fp, err := os.OpenFile(options.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			logger.Errorf("Failed to open log file for writing: %v", err)
			return
		}
		host.SetLogging(fp)
	}

	go host.Serve()

	// Construct interrupt handler
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	<-sig // Wait for ^C signal
	logger.Warningf("Interrupt signal detected, shutting down.")
	os.Exit(0)
}

func fromFile(path string, handler func(line []byte) error) error {
	if path == "" {
		// Skip
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		err := handler(scanner.Bytes())
		if err != nil {
			return err
		}
	}
	return nil
}
