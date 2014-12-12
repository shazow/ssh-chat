package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"

	"github.com/alexcesaro/log"
	"github.com/alexcesaro/log/golog"
	"github.com/jessevdk/go-flags"
)

type Options struct {
	Verbose  []bool `short:"v" long:"verbose" description:"Show verbose logging."`
	Identity string `short:"i" long:"identity" description:"Private key to identify server with." default:"~/.ssh/id_rsa"`
	Bind     string `long:"bind" description:"Host and port to listen on." default:"0.0.0.0:22"`
	Admin    string `long:"admin" description:"Fingerprint of pubkey to mark as admin."`
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

	server, err := NewServer(privateKey)
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

	if options.Admin != "" {
		server.Op(options.Admin)
	}

	<-sig // Wait for ^C signal
	logger.Warningf("Interrupt signal detected, shutting down.")
	server.Stop()
}
