package main

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

const MSG_BUFFER = 10

const HELP_TEXT = `-> Available commands:
  /about
  /exit
  /help
  /list
  /nick $NAME
`

const ABOUT_TEXT = `-> ssh-chat is made by @shazow.

  It is a custom ssh server built in Go to serve a chat experience
  instead of a shell.

  Source: https://github.com/shazow/ssh-chat

  For more, visit shazow.net or follow at twitter.com/shazow
`

type Client struct {
	Server     *Server
	Conn       *ssh.ServerConn
	Msg        chan string
	Name       string
	term       *terminal.Terminal
	termWidth  int
	termHeight int
}

func NewClient(server *Server, conn *ssh.ServerConn) *Client {
	return &Client{
		Server: server,
		Conn:   conn,
		Name:   conn.User(),
		Msg:    make(chan string, MSG_BUFFER),
	}
}

func (c *Client) Resize(width int, height int) error {
	err := c.term.SetSize(width, height)
	if err != nil {
		logger.Errorf("Resize failed: %dx%d", width, height)
		return err
	}
	c.termWidth, c.termHeight = width, height
	return nil
}

func (c *Client) Rename(name string) {
	c.Name = name
	c.term.SetPrompt(fmt.Sprintf("[%s] ", name))
}

func (c *Client) handleShell(channel ssh.Channel) {
	defer channel.Close()

	go func() {
		for msg := range c.Msg {
			c.term.Write([]byte(msg))
		}
	}()

	for {
		line, err := c.term.ReadLine()
		if err != nil {
			break
		}

		parts := strings.SplitN(line, " ", 2)
		isCmd := strings.HasPrefix(parts[0], "/")

		if isCmd {
			switch parts[0] {
			case "/exit":
				channel.Close()
			case "/help":
				c.Msg <- HELP_TEXT
			case "/about":
				c.Msg <- ABOUT_TEXT
			case "/nick":
				if len(parts) == 2 {
					c.Server.Rename(c, parts[1])
				} else {
					c.Msg <- fmt.Sprintf("-> Missing $NAME from: /nick $NAME\r\n")
				}
			case "/list":
				c.Msg <- fmt.Sprintf("-> Connected: %s\r\n", strings.Join(c.Server.List(nil), ","))
			default:
				c.Msg <- fmt.Sprintf("-> Invalid command: %s\r\n", line)
			}
			continue
		}

		msg := fmt.Sprintf("%s: %s\r\n", c.Name, line)
		c.Server.Broadcast(msg, c)
	}

}

func (c *Client) handleChannels(channels <-chan ssh.NewChannel) {
	prompt := fmt.Sprintf("[%s] ", c.Name)

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

		c.term = terminal.NewTerminal(channel, prompt)

		go func(in <-chan *ssh.Request) {
			defer channel.Close()
			hasShell := false
			for req := range in {
				var width, height int
				var ok bool

				switch req.Type {
				case "shell":
					if c.term != nil && !hasShell {
						go c.handleShell(channel)
						ok = true
						hasShell = true
					}
				case "pty-req":
					width, height, ok = parsePtyRequest(req.Payload)
					if ok {
						err := c.Resize(width, height)
						ok = err == nil
					}
				case "window-change":
					width, height, ok = parseWinchRequest(req.Payload)
					if ok {
						err := c.Resize(width, height)
						ok = err == nil
					}
				}

				if req.WantReply {
					req.Reply(ok, nil)
				}
			}
		}(requests)

		// We don't care about other channels?
		return
	}
}
