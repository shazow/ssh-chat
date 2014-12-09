package main

import (
	"fmt"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

const MSG_BUFFER = 10

type Client struct {
	Server *Server
	Msg    chan string
	Name   string
}

func NewClient(server *Server, name string) *Client {
	if name == "" {
		name = "Anonymoose"
	}

	return &Client{
		Server: server,
		Name:   name,
		Msg:    make(chan string, MSG_BUFFER),
	}
}

func (c *Client) handleShell(channel ssh.Channel) {
	defer channel.Close()

	term := terminal.NewTerminal(channel, "")

	for {
		line, err := term.ReadLine()
		if err != nil {
			break
		}

		switch line {
		case "/exit":
			channel.Close()
		}

		msg := fmt.Sprintf("%s: %s\r\n", c.Name, line)
		c.Server.Broadcast(msg)
	}
}

func (c *Client) handleChannels(channels <-chan ssh.NewChannel) {
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
				ok := false
				switch req.Type {
				case "shell":
					if len(req.Payload) == 0 {
						ok = true
					}
				case "pty-req":
					// Setup PTY
					ok = true
				}
				req.Reply(ok, nil)
			}
		}(requests)

		go c.handleShell(channel)

		go func() {
			for msg := range c.Msg {
				channel.Write([]byte(msg))
			}
		}()

		// We don't care about other channels?
		return
	}
}
