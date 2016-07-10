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

	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh"

	"github.com/shazow/ssh-chat/auth"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/host"
	"github.com/shazow/ssh-chat/log"
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

func fail(code int, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(code)
}

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

	numVerbose := len(options.Verbose)
	log.Init(numVerbose)

	privateKeyPath := options.Identity
	if strings.HasPrefix(privateKeyPath, "~/") {
		user, err := user.Current()
		if err == nil {
			privateKeyPath = strings.Replace(privateKeyPath, "~", user.HomeDir, 1)
		}
	}

	privateKey, err := ReadPrivateKey(privateKeyPath)
	if err != nil {
		fail(2, "Couldn't read private key: %v\n", err)
	}

	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		fail(3, "Failed to parse key: %v\n", err)
	}

	auth := auth.NewAuth()
	config := sshd.MakeAuth(auth)
	config.AddHostKey(signer)

	s, err := sshd.ListenSSH(options.Bind, config)
	if err != nil {
		fail(4, "Failed to listen on socket: %v\n", err)
	}
	defer s.Close()
	s.RateLimit = sshd.NewInputLimiter

	fmt.Printf("Listening for connections on %v\n", s.Addr().String())

	host := host.NewHost(s, auth)
	host.SetTheme(message.Themes[0])

	err = fromFile(options.Admin, func(line []byte) error {
		key, _, _, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			return err
		}
		auth.Op(key, 0)
		return nil
	})
	if err != nil {
		fail(5, "Failed to load admins: %v\n", err)
	}

	err = fromFile(options.Whitelist, func(line []byte) error {
		key, _, _, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			return err
		}
		auth.Whitelist(key, 0)
		return nil
	})
	if err != nil {
		fail(6, "Failed to load whitelist: %v\n", err)
	}

	if options.Motd != "" {
		motd, err := ioutil.ReadFile(options.Motd)
		if err != nil {
			fail(7, "Failed to load MOTD file: %v\n", err)
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
			fail(8, "Failed to open log file for writing: %v", err)
		}
		host.SetLogging(fp)
	}

	go host.Serve()

	// Construct interrupt handler
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	<-sig // Wait for ^C signal
	fmt.Fprintln(os.Stderr, "Interrupt signal detected, shutting down.")
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
