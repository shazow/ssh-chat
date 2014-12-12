package main

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

const MSG_BUFFER int = 10

const HELP_TEXT string = `-> Available commands:
   /about
   /exit
   /help
   /list
   /nick $NAME
   /whois $NAME
`

const ABOUT_TEXT string = `-> ssh-chat is made by @shazow.

   It is a custom ssh server built in Go to serve a chat experience
   instead of a shell.

   Source: https://github.com/shazow/ssh-chat

   For more, visit shazow.net or follow at twitter.com/shazow
`

type Client struct {
	Server        *Server
	Conn          *ssh.ServerConn
	Msg           chan string
	Name          string
	Op            bool
	ready         chan struct{}
	term          *terminal.Terminal
	termWidth     int
	termHeight    int
	silencedUntil time.Time
}

func NewClient(server *Server, conn *ssh.ServerConn) *Client {
	return &Client{
		Server: server,
		Conn:   conn,
		Name:   conn.User(),
		Msg:    make(chan string, MSG_BUFFER),
		ready:  make(chan struct{}, 1),
	}
}

func (c *Client) Write(msg string) {
	c.term.Write([]byte(msg + "\r\n"))
}

func (c *Client) WriteLines(msg []string) {
	for _, line := range msg {
		c.Write(line)
	}
}

func (c *Client) IsSilenced() bool {
	return c.silencedUntil.After(time.Now())
}

func (c *Client) Silence(d time.Duration) {
	c.silencedUntil = time.Now().Add(d)
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

	// FIXME: This shouldn't live here, need to restructure the call chaining.
	c.Server.Add(c)
	go func() {
		// Block until done, then remove.
		c.Conn.Wait()
		c.Server.Remove(c)
	}()

	go func() {
		for msg := range c.Msg {
			c.Write(msg)
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
				c.WriteLines(strings.Split(HELP_TEXT, "\n"))
			case "/about":
				c.WriteLines(strings.Split(ABOUT_TEXT, "\n"))
			case "/nick":
				if len(parts) == 2 {
					c.Server.Rename(c, parts[1])
				} else {
					c.Msg <- fmt.Sprintf("-> Missing $NAME from: /nick $NAME")
				}
			case "/whois":
				if len(parts) == 2 {
					client := c.Server.Who(parts[1])
					c.Msg <- fmt.Sprintf("-> %s is %s via %s", client.Name, client.Conn.RemoteAddr(), client.Conn.ClientVersion())
				} else {
					c.Msg <- fmt.Sprintf("-> Missing $NAME from: /whois $NAME")
				}
			case "/list":
				names := c.Server.List(nil)
				c.Msg <- fmt.Sprintf("-> %d connected: %s", len(names), strings.Join(names, ", "))
			default:
				c.Msg <- fmt.Sprintf("-> Invalid command: %s", line)
			}
			continue
		}

		msg := fmt.Sprintf("%s: %s", c.Name, line)
		if c.IsSilenced() {
			c.Msg <- fmt.Sprintf("-> Message rejected, silenced.")
			continue
		}
		c.Server.Broadcast(msg, c)
	}

}

func (c *Client) handleChannels(channels <-chan ssh.NewChannel) {
	prompt := fmt.Sprintf("[%s] ", c.Name)

	hasShell := false

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
		defer channel.Close()

		c.term = terminal.NewTerminal(channel, prompt)
		for req := range requests {
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
	}
}
