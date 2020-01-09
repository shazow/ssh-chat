package message

import (
	"fmt"
	"strings"
	"time"
)

// Message is an interface for messages.
type Message interface {
	Render(*Theme) string
	String() string
	Command() string
	Timestamp() time.Time
}

type MessageTo interface {
	Message
	To() *User
}

type MessageFrom interface {
	Message
	From() *User
}

func ParseInput(body string, from *User) Message {
	m := NewPublicMsg(body, from)
	cmd, isCmd := m.ParseCommand()
	if isCmd {
		return cmd
	}
	return m
}

// Msg is a base type for other message types.
type Msg struct {
	body      string
	timestamp time.Time
	// TODO: themeCache *map[*Theme]string
}

func NewMsg(body string) *Msg {
	return &Msg{
		body:      body,
		timestamp: time.Now(),
	}
}

// Render message based on a theme.
func (m Msg) Render(t *Theme) string {
	// TODO: Render based on theme
	// TODO: Cache based on theme
	return m.String()
}

func (m Msg) String() string {
	return m.body
}

func (m Msg) Command() string {
	return ""
}

func (m Msg) Timestamp() time.Time {
	return m.timestamp
}

// PublicMsg is any message from a user sent to the room.
type PublicMsg struct {
	Msg
	from *User
}

func NewPublicMsg(body string, from *User) PublicMsg {
	return PublicMsg{
		Msg: Msg{
			body:      body,
			timestamp: time.Now(),
		},
		from: from,
	}
}

func (m PublicMsg) From() *User {
	return m.from
}

func (m PublicMsg) ParseCommand() (*CommandMsg, bool) {
	// Check if the message is a command
	if !strings.HasPrefix(m.body, "/") {
		return nil, false
	}

	// Parse
	// TODO: Handle quoted fields properly
	fields := strings.Fields(m.body)
	command, args := fields[0], fields[1:]
	msg := CommandMsg{
		PublicMsg: m,
		command:   command,
		args:      args,
	}
	return &msg, true
}

func (m PublicMsg) Render(t *Theme) string {
	if t == nil {
		return m.String()
	}

	return fmt.Sprintf("%s: %s", t.ColorName(m.from), m.body)
}

// RenderFor renders the message for other users to see.
func (m PublicMsg) RenderFor(cfg UserConfig) string {
	if cfg.Highlight == nil || cfg.Theme == nil {
		return m.Render(cfg.Theme)
	}

	if !cfg.Highlight.MatchString(m.body) {
		return m.Render(cfg.Theme)
	}

	body := cfg.Highlight.ReplaceAllString(m.body, cfg.Theme.Highlight("${1}"))
	if cfg.Bell {
		body += Bel
	}
	return fmt.Sprintf("%s: %s", cfg.Theme.ColorName(m.from), body)
}

// RenderSelf renders the message for when it's echoing your own message.
func (m PublicMsg) RenderSelf(cfg UserConfig) string {
	return fmt.Sprintf("[%s] %s", cfg.Theme.ColorName(m.from), m.body)
}

func (m PublicMsg) String() string {
	return fmt.Sprintf("%s: %s", m.from.Name(), m.body)
}

// EmoteMsg is a /me message sent to the room. It specifically does not
// extend PublicMsg because it doesn't implement MessageFrom to allow the
// sender to see the emote.
type EmoteMsg struct {
	Msg
	from *User
}

func NewEmoteMsg(body string, from *User) *EmoteMsg {
	return &EmoteMsg{
		Msg: Msg{
			body:      body,
			timestamp: time.Now(),
		},
		from: from,
	}
}

func (m EmoteMsg) Render(t *Theme) string {
	return fmt.Sprintf("** %s %s", m.from.Name(), m.body)
}

func (m EmoteMsg) String() string {
	return m.Render(nil)
}

// PrivateMsg is a message sent to another user, not shown to anyone else.
type PrivateMsg struct {
	PublicMsg
	to *User
}

func NewPrivateMsg(body string, from *User, to *User) PrivateMsg {
	return PrivateMsg{
		PublicMsg: NewPublicMsg(body, from),
		to:        to,
	}
}

func (m PrivateMsg) To() *User {
	return m.to
}

func (m PrivateMsg) Render(t *Theme) string {
	s := fmt.Sprintf("[PM from %s] %s", m.from.Name(), m.body)
	if t == nil {
		return s
	}
	return t.ColorPM(s)
}

func (m PrivateMsg) String() string {
	return m.Render(nil)
}

// SystemMsg is a response sent from the server directly to a user, not shown
// to anyone else. Usually in response to something, like /help.
type SystemMsg struct {
	Msg
	to *User
}

func NewSystemMsg(body string, to *User) *SystemMsg {
	return &SystemMsg{
		Msg: Msg{
			body:      body,
			timestamp: time.Now(),
		},
		to: to,
	}
}

func (m *SystemMsg) Render(t *Theme) string {
	if t == nil {
		return m.String()
	}
	return t.ColorSys(m.String())
}

func (m *SystemMsg) String() string {
	return fmt.Sprintf("-> %s", m.body)
}

func (m *SystemMsg) To() *User {
	return m.to
}

// AnnounceMsg is a message sent from the server to everyone, like a join or
// leave event.
type AnnounceMsg struct {
	Msg
}

func NewAnnounceMsg(body string) *AnnounceMsg {
	return &AnnounceMsg{
		Msg: Msg{
			body:      body,
			timestamp: time.Now(),
		},
	}
}

func (m AnnounceMsg) Render(t *Theme) string {
	if t == nil {
		return m.String()
	}
	return t.ColorSys(m.String())
}

func (m AnnounceMsg) String() string {
	return fmt.Sprintf(" * %s", m.body)
}

type CommandMsg struct {
	PublicMsg
	command string
	args    []string
}

func (m CommandMsg) Command() string {
	return m.command
}

func (m CommandMsg) Args() []string {
	return m.args
}

func (m CommandMsg) Body() string {
	return m.body
}

func (m CommandMsg) RenderSelf(cfg UserConfig) string {
	return m.body
}
