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
const DEFAULT_CHANNEL = "default"

const HELP_TEXT string = SYSTEM_MESSAGE_FORMAT + `-> Available commands:
   /about               - About this chat.
   /exit                - Exit the chat.
   /help                - Show this help text.
   /list                - List the users that are currently connected.
   /beep                - Enable BEL notifications on mention.
   /me $ACTION          - Show yourself doing an action.
   /nick $NAME          - Rename yourself to a new name.
   /whois $NAME         - Display information about another connected user.
   /msg $NAME $MESSAGE  - Sends a private message to a user.
   /motd                - Prints the Message of the Day
   /join $CHANNEL       - Join the specified channel.
   /channel             - Print the name of the current channel.
` + RESET

const OP_HELP_TEXT string = SYSTEM_MESSAGE_FORMAT + `-> Available operator commands:
   /ban $NAME       - Banish a user from the chat
   /kick $NAME      - Kick em' out.
   /op $NAME        - Promote a user to server operator
   /silence $NAME   - Revoke a user's ability to speak
   /ban $NAME           - Banish a user from the chat
   /kick $NAME          - Kick em' out.
   /op $NAME            - Promote a user to server operator.
   /silence $NAME       - Revoke a user's ability to speak.
   /motd $MESSAGE    - Sets the Message of the Day
` + RESET

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
	Channel       string
}

func NewClient(server *Server, conn *ssh.ServerConn) *Client {
	return &Client{
		Server:  server,
		Conn:    conn,
		Name:    conn.User(),
		Color:   RandomColor256(),
		Msg:     make(chan string, MSG_BUFFER),
		ready:   make(chan struct{}, 1),
		lastTX:  time.Now(),
		Channel: DEFAULT_CHANNEL,
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

	for {
		line, err := c.term.ReadLine()
		if err != nil {
			break
		}

		parts := strings.SplitN(line, " ", 3)
		isCmd := strings.HasPrefix(parts[0], "/")

		if isCmd {
			// TODO: Factor this out.
			switch parts[0] {
			case "/test-colors": // Shh, this command is a secret!
				c.Write(ColorString("32", "Lorem ipsum dolor sit amet,"))
				c.Write("consectetur " + ColorString("31;1", "adipiscing") + " elit.")
			case "/exit":
				channel.Close()
			case "/help":
				c.WriteLines(strings.Split(HELP_TEXT, "\n"))
				if c.Server.IsOp(c) {
					c.WriteLines(strings.Split(OP_HELP_TEXT, "\n"))
				}
			case "/about":
				c.WriteLines(strings.Split(ABOUT_TEXT, "\n"))
			case "/uptime":
				c.Write(c.Server.Uptime())
			case "/beep":
				c.beepMe = !c.beepMe
				if c.beepMe {
					c.SysMsg("I'll beep you good.")
				} else {
					c.SysMsg("No more beeps. :(")
				}
			case "/me":
				me := strings.TrimLeft(line, "/me")
				if me == "" {
					me = " is at a loss for words."
				}
				msg := fmt.Sprintf("** %s%s", c.ColoredName(), me)
				if c.IsSilenced() || len(msg) > 1000 {
					c.SysMsg("Message rejected.")
				} else {
					channel := Client{Channel: c.Channel}
					c.Server.Broadcast(msg, &channel)
				}
			case "/nick":
				if len(parts) == 2 {
					c.Server.Rename(c, parts[1])
				} else {
					c.SysMsg("Missing $NAME from: /nick $NAME")
				}
			case "/whois":
				if len(parts) == 2 {
					client := c.Server.Who(parts[1])
					if client != nil {
						version := RE_STRIP_TEXT.ReplaceAllString(string(client.Conn.ClientVersion()), "")
						if len(version) > 100 {
							version = "Evil Jerk with a superlong string"
						}
						c.SysMsg("%s is %s via %s", client.ColoredName(), client.Fingerprint(), version)
					} else {
						c.SysMsg("No such name: %s", parts[1])
					}
				} else {
					c.SysMsg("Missing $NAME from: /whois $NAME")
				}
			case "/list":
				names := ""
				nameList := c.Server.ListChannel(c.Channel)
				for _, name := range nameList {
					names += c.Server.Who(name).ColoredName() + SYSTEM_MESSAGE_FORMAT + ", "
				}
				if len(names) > 2 {
					names = names[:len(names)-2]
				}
				c.SysMsg("%d connected in channel %s: %s", len(nameList), c.Channel, names)
			case "/ban":
				if !c.Server.IsOp(c) {
					c.SysMsg("You're not an admin.")
				} else if len(parts) != 2 {
					c.SysMsg("Missing $NAME from: /ban $NAME")
				} else {
					client := c.Server.Who(parts[1])
					if client == nil {
						c.SysMsg("No such name: %s", parts[1])
					} else {
						fingerprint := client.Fingerprint()
						client.SysMsg("Banned by %s.", c.ColoredName())
						c.Server.Ban(fingerprint, nil)
						client.Conn.Close()
						c.Server.Broadcast(fmt.Sprintf("* %s was banned by %s", parts[1], c.ColoredName()), nil)
					}
				}
			case "/op":
				if !c.Server.IsOp(c) {
					c.SysMsg("You're not an admin.")
				} else if len(parts) != 2 {
					c.SysMsg("Missing $NAME from: /op $NAME")
				} else {
					client := c.Server.Who(parts[1])
					if client == nil {
						c.SysMsg("No such name: %s", parts[1])
					} else {
						fingerprint := client.Fingerprint()
						client.SysMsg("Made op by %s.", c.ColoredName())
						c.Server.Op(fingerprint)
					}
				}
			case "/kick":
				if !c.Server.IsOp(c) {
					c.SysMsg("You're not an admin.")
				} else if len(parts) != 2 {
					c.SysMsg("Missing $NAME from: /kick $NAME")
				} else {
					client := c.Server.Who(parts[1])
					if client == nil {
						c.SysMsg("No such name: %s", parts[1])
					} else {
						client.SysMsg("Kicked by %s.", c.ColoredName())
						client.Conn.Close()
						c.Server.Broadcast(fmt.Sprintf("* %s was kicked by %s", parts[1], c.ColoredName()), nil)
					}
				}
			case "/silence":
				if !c.Server.IsOp(c) {
					c.SysMsg("You're not an admin.")
				} else if len(parts) < 2 {
					c.SysMsg("Missing $NAME from: /silence $NAME")
				} else {
					duration := time.Duration(5) * time.Minute
					if len(parts) >= 3 {
						parsedDuration, err := time.ParseDuration(parts[2])
						if err == nil {
							duration = parsedDuration
						}
					}
					client := c.Server.Who(parts[1])
					if client == nil {
						c.SysMsg("No such name: %s", parts[1])
					} else {
						client.Silence(duration)
						client.SysMsg("Silenced for %s by %s.", duration, c.ColoredName())
					}
				}
			case "/msg": /* Send a PM */
				/* Make sure we have a recipient and a message */
				if len(parts) < 2 {
					c.SysMsg("Missing $NAME from: /msg $NAME $MESSAGE")
					break
				} else if len(parts) < 3 {
					c.SysMsg("Missing $MESSAGE from: /msg $NAME $MESSAGE")
					break
				}
				/* Ask the server to send the message */
				if err := c.Server.Privmsg(parts[1], parts[2], c); nil != err {
					c.SysMsg("Unable to send message to %v: %v", parts[1], err)
				}
			case "/motd": /* print motd */
				if !c.Server.IsOp(c) {
					c.Server.MotdUnicast(c)
				} else if len(parts) < 2 {
					c.Server.MotdUnicast(c)
				} else {
					var newmotd string
					if len(parts) == 2 {
						newmotd = parts[1]
					} else {
						newmotd = parts[1] + " " + parts[2]
					}
					c.Server.SetMotd(c, newmotd)
					c.Server.MotdBroadcast(c)
				}
			case "/join":
				if len(parts) < 2 {
					c.SysMsg("Missing $CHANNEL from: /join $CHANNEL.")
					break
				}
				if parts[1] == "" {
					c.SysMsg("The name of the $CHANNEL can not be empty.")
					break
				}

				c.Server.setChannel(c, parts[1])
			case "/channel":
				c.SysMsg("You are currently in channel %s.", c.Channel)
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

func (c *Client) setChannel(channel string) {
	c.Channel = channel
	c.SysMsg("joined channel %s.", channel)
}
