package message

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"regexp"
	"sync"
	"time"
)

const messageBuffer = 5
const messageTimeout = 5 * time.Second
const reHighlight = `\b(%s)\b`

var ErrUserClosed = errors.New("user closed")

// User definition, implemented set Item interface and io.Writer
type User struct {
	colorIdx int
	joined   time.Time
	msg      chan Message
	done     chan struct{}

	screen    io.WriteCloser
	closeOnce sync.Once

	mu      sync.Mutex
	name    string
	config  UserConfig
	replyTo *User // Set when user gets a /msg, for replying.
}

func NewUser(name string) *User {
	u := User{
		name:   name,
		config: DefaultUserConfig,
		joined: time.Now(),
		msg:    make(chan Message, messageBuffer),
		done:   make(chan struct{}),
	}
	u.setColorIdx(rand.Int())

	return &u
}

func NewUserScreen(name string, screen io.WriteCloser) *User {
	u := NewUser(name)
	u.screen = screen

	return u
}

func (u *User) Config() UserConfig {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.config
}

func (u *User) SetConfig(cfg UserConfig) {
	u.mu.Lock()
	u.config = cfg
	u.mu.Unlock()
}

func (u *User) ID() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return SanitizeName(u.name)
}

func (u *User) Name() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.name
}

func (u *User) Joined() time.Time {
	return u.joined
}

// Rename the user with a new Identifier.
func (u *User) SetName(name string) {
	u.mu.Lock()
	u.name = name
	u.setColorIdx(rand.Int())
	u.mu.Unlock()
}

// ReplyTo returns the last user that messaged this user.
func (u *User) ReplyTo() *User {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.replyTo
}

// SetReplyTo sets the last user to message this user.
func (u *User) SetReplyTo(user *User) {
	// TODO: Use UserConfig.ReplyTo string
	u.mu.Lock()
	defer u.mu.Unlock()
	u.replyTo = user
}

// setColorIdx will set the colorIdx to a specific value, primarily used for
// testing.
func (u *User) setColorIdx(idx int) {
	u.colorIdx = idx
}

// Disconnect user, stop accepting messages
func (u *User) Close() {
	u.closeOnce.Do(func() {
		if u.screen != nil {
			u.screen.Close()
		}
		// close(u.msg) TODO: Close?
		close(u.done)
	})
}

// Consume message buffer into the handler. Will block, should be called in a
// goroutine.
func (u *User) Consume() {
	for {
		select {
		case <-u.done:
			return
		case m, ok := <-u.msg:
			if !ok {
				return
			}
			u.HandleMsg(m)
		}
	}
}

// Consume one message and stop, mostly for testing
// TODO: Stop using it and remove it.
func (u *User) ConsumeOne() Message {
	return <-u.msg
}

// Check if there are pending messages, used for testing
// TODO: Stop using it and remove it.
func (u *User) HasMessages() bool {
	select {
	case msg := <-u.msg:
		u.msg <- msg
		return true
	default:
		return false
	}
}

// SetHighlight sets the highlighting regular expression to match string.
func (u *User) SetHighlight(s string) error {
	re, err := regexp.Compile(fmt.Sprintf(reHighlight, s))
	if err != nil {
		return err
	}
	u.mu.Lock()
	u.config.Highlight = re
	u.mu.Unlock()
	return nil
}

func (u *User) render(m Message) string {
	cfg := u.Config()
	switch m := m.(type) {
	case PublicMsg:
		return m.RenderFor(cfg) + Newline
	case PrivateMsg:
		u.SetReplyTo(m.From())
		return m.Render(cfg.Theme) + Newline
	default:
		return m.Render(cfg.Theme) + Newline
	}
}

// HandleMsg will render the message to the screen, blocking.
func (u *User) HandleMsg(m Message) error {
	r := u.render(m)
	_, err := u.screen.Write([]byte(r))
	if err != nil {
		logger.Printf("Write failed to %s, closing: %s", u.Name(), err)
		u.Close()
	}
	return err
}

// Add message to consume by user
func (u *User) Send(m Message) error {
	select {
	case <-u.done:
		return ErrUserClosed
	case u.msg <- m:
	case <-time.After(messageTimeout):
		logger.Printf("Message buffer full, closing: %s", u.Name())
		u.Close()
		return ErrUserClosed
	}
	return nil
}

// Prompt renders a theme-colorized prompt string.
func (u *User) Prompt() string {
	name := u.Name()
	cfg := u.Config()
	if cfg.Theme != nil {
		name = cfg.Theme.ColorName(u)
	}
	return fmt.Sprintf("[%s] ", name)
}

// Container for per-user configurations.
type UserConfig struct {
	Highlight *regexp.Regexp
	Bell      bool
	Quiet     bool
	Theme     *Theme
}

// Default user configuration to use
var DefaultUserConfig UserConfig

func init() {
	DefaultUserConfig = UserConfig{
		Bell:  true,
		Quiet: false,
	}

	// TODO: Seed random?
}
