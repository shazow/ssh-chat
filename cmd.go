package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"

	"github.com/alexcesaro/log"
	"github.com/alexcesaro/log/golog"
	"github.com/jessevdk/go-flags"
)

type Options struct {
	Verbose   []bool   `short:"v" long:"verbose" description:"Show verbose logging."`
	Identity  string   `short:"i" long:"identity" description:"Private key to identify server with." default:"~/.ssh/id_rsa"`
	Bind      string   `long:"bind" description:"Host and port to listen on." default:"0.0.0.0:22"`
	Admin     []string `long:"admin" description:"Fingerprint of pubkey to mark as admin."`
	Whitelist string   `long:"whitelist" description:"Optional file of pubkey fingerprints that are allowed to connect"`
}

var logLevels = []log.Level{
	log.Warning,
	log.Info,
	log.Debug,
}

func main() {
	options := Options{}
	parser := flags.NewParser(&options, flags.Default)

	p, err := parser.Parse()
	if err != nil {
		if p == nil {
			fmt.Print(err)
		}
		return
	}

	// Initialize seed for random colors
	RandomColorInit()

	// Figure out the log level
	numVerbose := len(options.Verbose)
	if numVerbose > len(logLevels) {
		numVerbose = len(logLevels)
	}

	logLevel := logLevels[numVerbose]
	logger = golog.New(os.Stderr, logLevel)

	privateKey, err := ioutil.ReadFile(options.Identity)
	if err != nil {
		logger.Errorf("Failed to load identity: %v", err)
		return
	}

	whitelistArray := []string{}
	if len(options.Whitelist) != 0 {
		file, err := os.Open(options.Whitelist)
		if err != nil {
			logger.Errorf("could not open whiltelist file")
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			whitelistArray = append(whitelistArray, scanner.Text())
		}
		logger.Errorf("%v", whitelistArray)

	}

	server, err := NewServer(privateKey, whitelistArray)
	if err != nil {
		logger.Errorf("Failed to create server: %v", err)
		return
	}

	// Construct interrupt handler
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	err = server.Start(options.Bind)
	if err != nil {
		logger.Errorf("Failed to start server: %v", err)
		return
	}

	for _, fingerprint := range options.Admin {
		server.Op(fingerprint)
	}

	<-sig // Wait for ^C signal
	logger.Warningf("Interrupt signal detected, shutting down.")
	server.Stop()
}
