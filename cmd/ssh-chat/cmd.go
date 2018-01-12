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
	"github.com/fsnotify/fsnotify"
	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh"

	"github.com/shazow/ssh-chat"
	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)
import _ "net/http/pprof"

// Version of the binary, assigned during build.
var Version string = "dev"

// Options contains the flag options
type Options struct {
	Verbose   []bool `short:"v" long:"verbose" description:"Show verbose logging."`
	Version   bool   `long:"version" description:"Print version and exit."`
	Identity  string `short:"i" long:"identity" description:"Private key to identify server with." default:"~/.ssh/id_rsa"`
	Bind      string `long:"bind" description:"Host and port to listen on." default:"0.0.0.0:2022"`
	Mods      string `long:"moderators" description:"File of public keys who are moderators."`
	Admins    string `long:"admins" description:"File of public keys who are admins."`
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

	if options.Version {
		fmt.Println(Version)
		os.Exit(0)
	}

	// Figure out the log level
	numVerbose := len(options.Verbose)
	if numVerbose > len(logLevels) {
		numVerbose = len(logLevels) - 1
	}

	logLevel := logLevels[numVerbose]
	sshchat.SetLogger(golog.New(os.Stderr, logLevel))

	if logLevel == log.Debug {
		// Enable logging from submodules
		chat.SetLogger(os.Stderr)
		sshd.SetLogger(os.Stderr)
		message.SetLogger(os.Stderr)
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
		fail(2, "Couldn't read private key: %v\n", err)
	}

	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		fail(3, "Failed to parse key: %v\n", err)
	}

	auth := sshchat.NewAuth()
	config := sshd.MakeAuth(auth)
	config.AddHostKey(signer)

	s, err := sshd.ListenSSH(options.Bind, config)
	if err != nil {
		fail(4, "Failed to listen on socket: %v\n", err)
	}
	defer s.Close()
	s.RateLimit = sshd.NewInputLimiter

	fmt.Printf("Listening for connections on %v\n", s.Addr().String())

	host := sshchat.NewHost(s, auth)
	host.SetTheme(message.Themes[0])
	host.Version = Version

	err = fromFile(options.Admins, func(line []byte) error {
		key, _, _, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			return err
		}
		auth.Master(key, 0)
		return nil
	})
	if err != nil {
		fail(5, "Failed to load admins: %v\n", err)
	}
	err = fromFile(options.Mods, func(line []byte) error {
		key, _, _, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			return err
		}
		auth.Op(key, 0)
		return nil
	})
	if err != nil {
		fail(5, "Failed to load mods: %v\n", err)
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
		motdString := string(motd)
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

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()

	go host.Serve()
	go func() {
		for event := range watcher.Events {
			if event.Op&fsnotify.Write == fsnotify.Write {
				if event.Name == options.Admins {
					err = fromFile(options.Admins, func(line []byte) error {
						key, _, _, _, err := ssh.ParseAuthorizedKey(line)
						if err != nil {
							return err
						}
						auth.Master(key, 0)
						return nil
					})
				}
				if event.Name == options.Mods {
					err = fromFile(options.Mods, func(line []byte) error {
						key, _, _, _, err := ssh.ParseAuthorizedKey(line)
						if err != nil {
							return err
						}
						auth.Op(key, 0)
						return nil
					})
				}
			}
		}
	}()
	if len(options.Admins) > 0 {
		watcher.Add(options.Admins)
	}
	if len(options.Mods) > 0 {
		watcher.Add(options.Mods)
	}

	// Construct interrupt handler
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	<-sig // Wait for ^C signal
	fmt.Fprintln(os.Stderr, "Interrupt signal detected, shutting down.")
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
