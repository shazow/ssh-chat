package main

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

const MSG_BUFFER int = 50
const MAX_MSG_LENGTH int = 512

const ABOUT_TEXT string = SYSTEM_MESSAGE_FORMAT + `-> ssh-chat is made by @shazow.

   It is a custom ssh server built in Go to serve a chat experience
   instead of a shell.

   Source: https://github.com/shazow/ssh-chat

   For more, visit shazow.net or follow at twitter.com/shazow
` + RESET

const REQUIRED_WAIT time.Duration = time.Second / 2

type Client struct {
	Server        *Server
	Conn          *ssh.ServerConn
	Msg           chan string
	Name          string
	Color         string
	Op            bool
	ready         chan struct{}
	term          *terminal.Terminal
	termWidth     int
	termHeight    int
	silencedUntil time.Time
	lastTX        time.Time
	beepMe        bool
}

func NewClient(server *Server, conn *ssh.ServerConn) *Client {
	return &Client{
		Server: server,
		Conn:   conn,
		Name:   conn.User(),
		Color:  RandomColor256(),
		Msg:    make(chan string, MSG_BUFFER),
		ready:  make(chan struct{}, 1),
		lastTX: time.Now(),
	}
}

func (c *Client) ColoredName() string {
	return ColorString(c.Color, c.Name)
}

func (c *Client) SysMsg(msg string, args ...interface{}) {
	c.Msg <- ContinuousFormat(SYSTEM_MESSAGE_FORMAT, "-> "+fmt.Sprintf(msg, args...))
}

func (c *Client) Write(msg string) {
	c.term.Write([]byte(msg + "\r\n"))
}

func (c *Client) WriteLines(msg []string) {
	for _, line := range msg {
		c.Write(line)
	}
}

func (c *Client) Send(msg string) {
	if len(msg) > MAX_MSG_LENGTH {
		return
	}
	select {
	case c.Msg <- msg:
	default:
		logger.Errorf("Msg buffer full, dropping: %s (%s)", c.Name, c.Conn.RemoteAddr())
		c.Conn.Close()
	}
}

func (c *Client) SendLines(msg []string) {
	for _, line := range msg {
		c.Send(line)
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
	c.term.SetPrompt(fmt.Sprintf("[%s] ", c.ColoredName()))
}

func (c *Client) Fingerprint() string {
	return c.Conn.Permissions.Extensions["fingerprint"]
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

HandleLine:
	for {
		line, err := c.term.ReadLine()
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)

		parts := strings.SplitN(line, " ", 3)
		argc := len(parts)
		isCmd := strings.HasPrefix(parts[0], "/")

		if isCmd {
			if cmds, exists := commands[parts[0]]; exists {
				for _, cmd := range cmds {
					if cmd.Optional || (cmd.HasMsg && cmd.Args <= argc) || (!cmd.HasMsg && cmd.Args == argc) {
						if cmd.MustBeAdmin && !c.Server.IsOp(c) {
							c.SysMsg("You're not an admin.")
						} else {
							cmd.Invoke(c, parts)
						}
					} else if argc < cmd.Args {
						args := strings.Split(cmd.Spec, " ")
						c.SysMsg(fmt.Sprintf("Missing %s from: %s", args[argc], cmd.Spec))
					} else {
						continue
					}
					continue HandleLine
				}
			}
			// TODO: Factor this out.
			switch parts[0] {
			case "/test-colors": // Shh, this command is a secret!
				c.Write(ColorString("32", "Lorem ipsum dolor sit amet,"))
				c.Write("consectetur " + ColorString("31;1", "adipiscing") + " elit.")
			case "/exit":
				channel.Close()
			case "/uptime":
				c.Write(c.Server.Uptime())

			default:
				c.SysMsg("Invalid command: %s", line)
			}
			continue
		}

		msg := fmt.Sprintf("%s: %s", c.ColoredName(), line)
		/* Rate limit */
		if time.Now().Sub(c.lastTX) < REQUIRED_WAIT {
			c.SysMsg("Rate limiting in effect.")
			continue
		}
		if c.IsSilenced() || len(msg) > 1000 || len(line) < 1 {
			c.SysMsg("Message rejected.")
			continue
		}
		c.Server.Broadcast(msg, c)
		c.lastTX = time.Now()
	}

}

func (c *Client) handleChannels(channels <-chan ssh.NewChannel) {
	prompt := fmt.Sprintf("[%s] ", c.ColoredName())

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
		c.term.AutoCompleteCallback = c.Server.AutoCompleteFunction

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
