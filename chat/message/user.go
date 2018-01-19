package message

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"regexp"
	"sync"
	"time"

	"github.com/shazow/ssh-chat/set"
)

const messageBuffer = 5
const messageTimeout = 5 * time.Second
const reHighlight = `\b(%s)\b`
const timestampTimeout = 30 * time.Minute
const timestampLayout = "2006-01-02 15:04:05 UTC"

var ErrUserClosed = errors.New("user closed")

// User definition, implemented set Item interface and io.Writer
type User struct {
	Identifier
	Ignored  *set.Set
	colorIdx int
	joined   time.Time
	msg      chan Message
	done     chan struct{}

	screen    io.WriteCloser
	closeOnce sync.Once

	mu      sync.Mutex
	config  UserConfig
	replyTo *User     // Set when user gets a /msg, for replying.
	lastMsg time.Time // When the last message was rendered
}

func NewUser(identity Identifier) *User {
	u := User{
		Identifier: identity,
		config:     DefaultUserConfig,
		joined:     time.Now(),
		msg:        make(chan Message, messageBuffer),
		done:       make(chan struct{}),
		Ignored:    set.New(),
	}
	u.setColorIdx(rand.Int())

	return &u
}

func NewUserScreen(identity Identifier, screen io.WriteCloser) *User {
	u := NewUser(identity)
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

// Rename the user with a new Identifier.
func (u *User) SetID(id string) {
	u.Identifier.SetID(id)
	u.setColorIdx(rand.Int())
}

// ReplyTo returns the last user that messaged this user.
func (u *User) ReplyTo() *User {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.replyTo
}

// SetReplyTo sets the last user to message this user.
func (u *User) SetReplyTo(user *User) {
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
func (u *User) ConsumeOne() Message {
	return <-u.msg
}

// Check if there are pending messages, used for testing
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
	case *PrivateMsg:
		if cfg.Bell {
			return m.Render(cfg.Theme) + Bel + Newline
		}
		return m.Render(cfg.Theme) + Newline
	default:
		return m.Render(cfg.Theme) + Newline
	}
}

// writeMsg renders the message and attempts to write it, will Close the user
// if it fails.
func (u *User) writeMsg(m Message) error {
	r := u.render(m)
	_, err := u.screen.Write([]byte(r))
	if err != nil {
		logger.Printf("Write failed to %s, closing: %s", u.Name(), err)
		u.Close()
	}
	return err
}

// HandleMsg will render the message to the screen, blocking.
func (u *User) HandleMsg(m Message) error {
	u.mu.Lock()
	cfg := u.config
	lastMsg := u.lastMsg
	u.lastMsg = m.Timestamp()
	injectTimestamp := !lastMsg.IsZero() && cfg.Timestamp && u.lastMsg.Sub(lastMsg) > timestampTimeout
	u.mu.Unlock()

	if injectTimestamp {
		// Inject a timestamp at most once every timestampTimeout between message intervals
		ts := NewSystemMsg(fmt.Sprintf("Timestamp: %s", m.Timestamp().UTC().Format(timestampLayout)), u)
		if err := u.writeMsg(ts); err != nil {
			return err
		}
	}

	return u.writeMsg(m)
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

// Container for per-user configurations.
type UserConfig struct {
	Highlight *regexp.Regexp
	Bell      bool
	Quiet     bool
	Timestamp bool
	Theme     *Theme
}

// Default user configuration to use
var DefaultUserConfig UserConfig

func init() {
	DefaultUserConfig = UserConfig{
		Bell:      true,
		Quiet:     false,
		Timestamp: false,
	}

	// TODO: Seed random?
}
