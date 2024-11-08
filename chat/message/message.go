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
	if cfg.Theme == nil {
		return fmt.Sprintf("[%s] %s", m.from.Name(), m.body)
	}
	return fmt.Sprintf("[%s] %s", cfg.Theme.ColorName(m.from), m.body)
}

func (m PublicMsg) String() string {
	return fmt.Sprintf("%s: %s", m.from.Name(), m.body)
}

// EmoteMsg is a /me message sent to the room.
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

func (m EmoteMsg) From() *User {
	return m.from
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

func (m PrivateMsg) From() *User {
	return m.from
}

func (m PrivateMsg) Render(t *Theme) string {
	format := "[PM from %s] %s"
	if t == nil {
		return fmt.Sprintf(format, m.from.ID(), m.body) 
	}
	s := fmt.Sprintf(format, m.from.Name(), m.body)
	return t.ColorPM(s)
}

func (m PrivateMsg) String() string {
	return m.Render(nil)
}

var _ Message = &SystemMsg{}

// SystemMsg is a response sent from the server directly to a user, not shown
// to anyone else. Usually in response to something, like /help.
type SystemMsg struct {
	parts     []string
	timestamp time.Time
	to        *User
}

var systemMessagePrefix = []string{"-> "}

func NewSystemMsg(body string, to *User) *SystemMsg {
	return &SystemMsg{
		parts:     append(systemMessagePrefix, body),
		timestamp: time.Now(),
		to:        to,
	}
}

func NewSystemMsgP(to *User, parts ...string) *SystemMsg {
	return &SystemMsg{
		to:        to,
		parts:     append(systemMessagePrefix, parts...),
		timestamp: time.Now(),
	}
}

func (m *SystemMsg) renderPlain() string {
	spArgs := make([]interface{}, len(m.parts))
	for i, arg := range m.parts {
		spArgs[i] = arg
	}
	return fmt.Sprint(spArgs...)
}

func (m *SystemMsg) Render(t *Theme) string {
	if t == nil {
		return m.String()
	}

	colPart := make([]interface{}, len(m.parts))
	for i, part := range m.parts {
		colPart[i] = t.ColorSys(part)
	}
	return fmt.Sprint(colPart...)
}

func (m *SystemMsg) String() string {
	return fmt.Sprintf("-> %s", m.renderPlain())
}

func (m *SystemMsg) To() *User {
	return m.to
}

func (m *SystemMsg) Command() string {
	return ""
}

func (m *SystemMsg) Timestamp() time.Time {
	return m.timestamp
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
