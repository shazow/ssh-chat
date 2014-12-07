// TODO: NoClientAuth

package main

import (
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
	"net"
)

type Server struct {
	sshConfig *ssh.ServerConfig
	sshSigner *ssh.Signer
	socket    *net.Listener
	done      chan struct{}
}

func NewServer(privateKey []byte) (*Server, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	config := ssh.ServerConfig{
		NoClientAuth: true,
	}
	config.AddHostKey(signer)

	server := Server{
		sshConfig: &config,
		sshSigner: &signer,
	}

	return &server, nil
}

func (s *Server) handleShell(channel ssh.Channel) {
	defer channel.Close()

	term := terminal.NewTerminal(channel, "")

	for {
		line, err := term.ReadLine()
		if err != nil {
			break
		}

		switch line {
		case "exit":
			channel.Close()
		}

		term.Write([]byte("you wrote: " + string(line) + "\r\n"))
	}
}

func (s *Server) handleChannels(channels <-chan ssh.NewChannel) {
	for ch := range channels {
		if t := ch.ChannelType(); t != "session" {
			ch.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
			continue
		}

		channel, requests, err := ch.Accept()
		if err != nil {
			logger.Errorf("Could not accept channel: %v", err)
			continue
		}

		go func(in <-chan *ssh.Request) {
			defer channel.Close()
			for req := range in {
				logger.Infof("Request: ", req.Type, string(req.Payload))

				ok := false
				switch req.Type {
				case "shell":
					// We don't accept any commands (Payload),
					// only the default shell.
					if len(req.Payload) == 0 {
						ok = true
					}
				case "pty-req":
					// Responding 'ok' here will let the client
					// know we have a pty ready for input
					ok = true
				case "window-change":
					continue //no response
				}
				req.Reply(ok, nil)
			}
		}(requests)

		go s.handleShell(channel)

		channel.Write([]byte("Hello"))
	}
}

func (s *Server) Start(laddr string) (<-chan struct{}, error) {
	// Once a ServerConfig has been configured, connections can be
	// accepted.
	socket, err := net.Listen("tcp", laddr)
	if err != nil {
		return nil, err
	}

	s.socket = &socket
	logger.Infof("Listening on %s", laddr)

	go func() {
		for {
			conn, err := socket.Accept()
			if err != nil {
				// TODO: Handle shutdown more gracefully.
				logger.Errorf("Failed to accept connection, aborting loop: %v", err)
				return
			}

			// From a standard TCP connection to an encrypted SSH connection
			sshConn, channels, requests, err := ssh.NewServerConn(conn, s.sshConfig)
			if err != nil {
				logger.Errorf("Failed to handshake: %v", err)
				continue
			}

			logger.Infof("Connection from: %s, %s, %s", sshConn.RemoteAddr(), sshConn.User(), sshConn.ClientVersion())

			go ssh.DiscardRequests(requests)
			go s.handleChannels(channels)
		}
	}()

	return s.done, nil
}

func (s *Server) Stop() error {
	err := (*s.socket).Close()
	s.done <- struct{}{}
	return err
}
