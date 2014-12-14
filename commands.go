package main

import (
	"fmt"
	"strings"
	"time"
)

var commands = make(map[string][]command)
var (
	HELP_TEXT    string = SYSTEM_MESSAGE_FORMAT + "-> Available commands:\n"
	OP_HELP_TEXT string = SYSTEM_MESSAGE_FORMAT + "-> Available operator commands:\n"
)

func init() {
	mustBeAdmin := false
	maxCmdSpecLength := 0
	addOpt := func(spec, desc string, invoke invocation, opt bool) {
		specParts := strings.Split(spec, " ")
		cmdName := specParts[0]
		if _, exists := commands[cmdName]; !exists {
			commands[cmdName] = make([]command, 0)
		}
		// used later for alignment in /help
		if len(spec) > maxCmdSpecLength {
			maxCmdSpecLength = len(spec)
		}
		commands[cmdName] = append(commands[cmdName], command{
			Spec:        spec,
			Info:        desc,
			MustBeAdmin: mustBeAdmin,
			HasMsg:      strings.HasSuffix(spec, "..."),
			Args:        len(specParts),
			Invoke:      invoke,
			Optional:    opt,
		})
	}
	add := func(spec, desc string, invoke invocation) {
		addOpt(spec, desc, invoke, false)
	}

	// define public commands
	add("/about", "About this chat.", AboutCmd)
	add("/exit", "Exit the chat.", ExitCmd)
	add("/help", "Show this help text.", HelpCmd)
	add("/list", "List the users that are currently connected.", ListCmd)
	add("/beep", "Enable/Disable BEL notifications on mention.", BeepCmd)
	add("/nick $NAME", "Rename yourself to a new name.", NickCmd)
	add("/whois $NAME", "Display information about another connected user.", WhoisCmd)
	add("/msg $NAME $MESSAGE...", "Sends a private message to a user.", MsgCmd)
	add("/motd", "Prints the Message of the Day.", PrintMotdCmd)
	addOpt("/me $ACTION...", "Show yourself doing an action.", MeCmd, true)

	// define admin commands
	mustBeAdmin = true
	add("/ban $NAME", "Banish a user from the chat.", BanCmd)
	add("/kick $NAME", "Kick em' out.", KickCmd)
	add("/op $NAME", "Promote a user to server operator.", OpCmd)
	add("/silence $NAME", "Revoke a user's ability to speak.", SilenceCmd)
	add("/motd $MESSAGE...", "Sets the Message of the Day.", SetMotdCmd)

	// generate help messages
	format := fmt.Sprintf("   %%-%ds - %%s\n", maxCmdSpecLength)
	for _, cmds := range commands {
		for _, cmd := range cmds {
			if cmd.MustBeAdmin {
				OP_HELP_TEXT += fmt.Sprintf(format, cmd.Spec, cmd.Info)
			} else {
				HELP_TEXT += fmt.Sprintf(format, cmd.Spec, cmd.Info)
			}
		}
	}
	HELP_TEXT += RESET
	OP_HELP_TEXT += RESET
}

func AboutCmd(c *Client, args []string) {
	c.WriteLines(strings.Split(ABOUT_TEXT, "\n"))
}

func ExitCmd(c *Client, args []string) {

}

func HelpCmd(c *Client, args []string) {
	c.WriteLines(strings.Split(HELP_TEXT, "\n"))
	if c.Server.IsOp(c) {
		c.WriteLines(strings.Split(OP_HELP_TEXT, "\n"))
	}
}

func ListCmd(c *Client, args []string) {
	names := ""
	nameList := c.Server.List(nil)
	for _, name := range nameList {
		names += c.Server.Who(name).ColoredName() + SYSTEM_MESSAGE_FORMAT + ", "
	}
	if len(names) > 2 {
		names = names[:len(names)-2]
	}
	c.SysMsg("%d connected: %s", len(nameList), names)
}

func BeepCmd(c *Client, args []string) {
	c.beepMe = !c.beepMe
	if c.beepMe {
		c.SysMsg("I'll beep you good.")
	} else {
		c.SysMsg("No more beeps. :(")
	}
}

func MeCmd(c *Client, args []string) {
	var action string
	if len(args) == 1 {
		action = "is at a loss for words."
	} else {
		action = strings.Join(args[1:], " ")
	}
	msg := fmt.Sprintf("** %s %s", c.ColoredName(), action)
	if c.IsSilenced() || len(msg) > 1000 {
		c.SysMsg("Message rejected.")
	} else {
		c.Server.Broadcast(msg, nil)
	}
}

func NickCmd(c *Client, parts []string) {
	c.Server.Rename(c, parts[1])
}

func WhoisCmd(c *Client, parts []string) {
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
}

func MsgCmd(c *Client, parts []string) {
	/* Ask the server to send the message */
	if err := c.Server.Privmsg(parts[1], parts[2], c); nil != err {
		c.SysMsg("Unable to send message to %v: %v", parts[1], err)
	}
}

func PrintMotdCmd(c *Client, args []string) {
	c.Server.MotdUnicast(c)
}

func BanCmd(c *Client, parts []string) {
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

func KickCmd(c *Client, parts []string) {
	client := c.Server.Who(parts[1])
	if client == nil {
		c.SysMsg("No such name: %s", parts[1])
	} else {
		client.SysMsg("Kicked by %s.", c.ColoredName())
		client.Conn.Close()
		c.Server.Broadcast(fmt.Sprintf("* %s was kicked by %s", parts[1], c.ColoredName()), nil)
	}
}

func OpCmd(c *Client, parts []string) {
	client := c.Server.Who(parts[1])
	if client == nil {
		c.SysMsg("No such name: %s", parts[1])
	} else {
		fingerprint := client.Fingerprint()
		client.SysMsg("Made op by %s.", c.ColoredName())
		c.Server.Op(fingerprint)
	}
}

func SilenceCmd(c *Client, parts []string) {
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

func SetMotdCmd(c *Client, parts []string) {
	var newmotd string
	if len(parts) == 2 {
		newmotd = parts[1]
	} else {
		newmotd = parts[1] + " " + parts[2]
	}
	c.Server.SetMotd(c, newmotd)
	c.Server.MotdBroadcast(c)
}

type invocation func(*Client, []string)

type command struct {
	Spec, Info  string
	MustBeAdmin bool
	HasMsg      bool
	Optional    bool
	Args        int
	Invoke      invocation
}
