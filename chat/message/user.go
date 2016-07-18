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
const reHighlight = `\b(%s)\b`

var ErrUserClosed = errors.New("user closed")

// User definition, implemented set Item interface and io.Writer
type User struct {
	Identifier
	Config   UserConfig
	colorIdx int
	joined   time.Time
	msg      chan Message
	done     chan struct{}

	mu        sync.RWMutex
	replyTo   *User // Set when user gets a /msg, for replying.
	screen    io.WriteCloser
	closeOnce sync.Once
}

func NewUser(identity Identifier) *User {
	u := User{
		Identifier: identity,
		Config:     *DefaultUserConfig,
		joined:     time.Now(),
		msg:        make(chan Message, messageBuffer),
		done:       make(chan struct{}, 1),
	}
	u.SetColorIdx(rand.Int())

	return &u
}

func NewUserScreen(identity Identifier, screen io.WriteCloser) *User {
	u := NewUser(identity)
	u.screen = screen

	return u
}

// Rename the user with a new Identifier.
func (u *User) SetId(id string) {
	u.Identifier.SetId(id)
	u.SetColorIdx(rand.Int())
}

// ReplyTo returns the last user that messaged this user.
func (u *User) ReplyTo() *User {
	return u.replyTo
}

// SetReplyTo sets the last user to message this user.
func (u *User) SetReplyTo(user *User) {
	u.replyTo = user
}

// ToggleQuietMode will toggle whether or not quiet mode is enabled
func (u *User) ToggleQuietMode() {
	u.Config.Quiet = !u.Config.Quiet
}

// SetColorIdx will set the colorIdx to a specific value, primarily used for
// testing.
func (u *User) SetColorIdx(idx int) {
	u.colorIdx = idx
}

// Block until user is closed
func (u *User) Wait() {
	<-u.done
}

// Disconnect user, stop accepting messages
func (u *User) Close() {
	u.closeOnce.Do(func() {
		u.mu.Lock()
		if u.screen != nil {
			u.screen.Close()
		}
		close(u.msg)
		close(u.done)
		u.msg = nil
		u.mu.Unlock()
	})
}

// Consume message buffer into an io.Writer. Will block, should be called in a
// goroutine.
// TODO: Not sure if this is a great API.
func (u *User) Consume() {
	for m := range u.msg {
		u.HandleMsg(m)
	}
}

// Consume one message and stop, mostly for testing
func (u *User) ConsumeOne() Message {
	return <-u.msg
}

// SetHighlight sets the highlighting regular expression to match string.
func (u *User) SetHighlight(s string) error {
	re, err := regexp.Compile(fmt.Sprintf(reHighlight, s))
	if err != nil {
		return err
	}
	u.Config.Highlight = re
	return nil
}

func (u *User) render(m Message) string {
	switch m := m.(type) {
	case *PublicMsg:
		return m.RenderFor(u.Config) + Newline
	case *PrivateMsg:
		u.SetReplyTo(m.From())
		return m.Render(u.Config.Theme) + Newline
	default:
		return m.Render(u.Config.Theme) + Newline
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
	u.mu.RLock()
	defer u.mu.RUnlock()
	select {
	case u.msg <- m:
	default:
		logger.Printf("Msg buffer full, closing: %s", u.Name())
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
	Theme     *Theme
}

// Default user configuration to use
var DefaultUserConfig *UserConfig

func init() {
	DefaultUserConfig = &UserConfig{
		Bell:  true,
		Quiet: false,
	}

	// TODO: Seed random?
}
