package apron

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/apron-chat/apron-server-go/apron"
	"github.com/apron-chat/apron-server-go/apron/server"
	"github.com/apron-chat/apron-server-go/apron/server/commands"

	"github.com/shazow/ssh-chat/chat"
)

// Commands (§4.1) are ssh-chat's: the gateway's Router knows each command
// that ssh-chat's /help lists, with its usage and help, for clients that
// list commands (ext:command_list), and runs every command line, known or
// not, as the member typed it. ssh-chat decides what it does, and who may
// do it: the Router makes none of its own checks of a command, only those
// of every input line (Gateway.before), and /help is ssh-chat's own.

// newRouter returns a Router of ssh-chat's commands.
func (g *Gateway) newRouter(registry chat.Commands) *commands.Router {
	// ssh-chat checks every command, and says what is wrong with it.
	r := &commands.Router{Before: g.before, Fallback: g.forward, SkipChecks: commands.CheckAll}
	r.MustAdd(commands.Command{
		Name:  "help",
		Forms: []commands.Form{{Help: "List the commands.", Group: commands.GroupAll}},
		Run:   g.forward,
	})
	var listed []*chat.Command
	for prefix, cmd := range registry {
		// As ssh-chat's /help: aliases and commands without help are
		// not listed, though they run.
		if prefix == cmd.Prefix && cmd.Help != "" && prefix != "/help" {
			listed = append(listed, cmd)
		}
	}
	slices.SortFunc(listed, func(a, b *chat.Command) int { return cmp.Compare(a.Prefix, b.Prefix) })
	for _, cmd := range listed {
		form := commands.Form{
			Hint:  strings.TrimSpace(cmd.PrefixHelp),
			Help:  cmd.Help,
			Group: commands.GroupAll,
		}
		if strings.Contains(cmd.PrefixHelp, "USER") {
			// A mention is written as the member's name (withNames).
			form.Uses = []string{commands.FieldMentions}
		}
		if cmd.Op {
			form.Group = commands.GroupAdmin
			form.May = g.isOp
		}
		if err := r.Add(commands.Command{
			Name:  strings.TrimPrefix(cmd.Prefix, "/"),
			Forms: []commands.Form{form},
			Run:   g.forward,
		}); err != nil {
			// A name the Router does not take still runs, unlisted.
			continue
		}
	}
	return r
}

// isOp reports whether the connection's member is an op, who may use op
// commands: in a listing, which is all the Router asks it for.
func (g *Gateway) isOp(_ context.Context, c *server.Conn, _ string) bool {
	s, ok := c.Data().(*session)
	return ok && g.host.IsOp(s.user)
}

// before checks a command line as ssh-chat checks a line of input, once
// its mentions are written as names, which is the line that runs: the
// call's Data.
func (g *Gateway) before(_ context.Context, call *commands.Call) error {
	if call.Params.RoomID != "" && call.Params.RoomID != g.room {
		return g.errOneRoom
	}
	s, _ := call.Conn.Data().(*session)
	text := strings.TrimSpace(g.withNames(call.Params.Body))
	if strings.ContainsAny(text, "\r\n") {
		return apron.InvalidParams("A command is one line")
	}
	if err := g.check(s, text); err != nil {
		return err
	}
	call.Data = text
	_, call.Args, _ = commands.Parse(text)
	return nil
}

// forward runs a command line as the member typed it, and answers once
// what it sends the member has been delivered.
func (g *Gateway) forward(ctx context.Context, call *commands.Call) error {
	s, _ := call.Conn.Data().(*session)
	line, _ := call.Data.(string)
	if err := g.start(s, line); err != nil {
		return err
	}
	// The command has run; what it sends the member is still on its way,
	// and comes before the result (§1, §4.1). Waiting for it can take a
	// while, so the result is sent later, and the connection goes on with
	// its next request meanwhile. ctx is the connection's, which ends with
	// it.
	if err := call.Reply.Later(); err != nil {
		return err
	}
	go func() {
		if err := g.settle(ctx, s.user); err != nil {
			call.Reply.Fail(err)
			return
		}
		call.Done()
	}()
	return nil
}
