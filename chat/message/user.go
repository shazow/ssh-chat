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

var ErrUserClosed = errors.New("user closed")

// User definition, implemented set Item interface and io.Writer
type User struct {
	Identifier
	OnChange func()
	Ignored  set.Interface
	Focused  set.Interface
	colorIdx int
	joined   time.Time
	msg      chan Message
	done     chan struct{}

	screen    io.WriteCloser
	closeOnce sync.Once

	mu         sync.Mutex
	config     UserConfig
	replyTo    *User     // Set when user gets a /msg, for replying.
	lastMsg    time.Time // When the last message was rendered.
	awayReason string    // Away reason, "" when not away.
	awaySince  time.Time // When away was set, 0 when not away.
}

func NewUser(identity Identifier) *User {
	u := User{
		Identifier: identity,
		config:     DefaultUserConfig,
		joined:     time.Now(),
		msg:        make(chan Message, messageBuffer),
		done:       make(chan struct{}),
		Ignored:    set.New(),
		Focused:    set.New(),
	}
	u.setColorIdx(rand.Int())

	return &u
}

func NewUserScreen(identity Identifier, screen io.WriteCloser) *User {
	u := NewUser(identity)
	u.screen = screen

	return u
}

func (u *User) Joined() time.Time {
	return u.joined
}

func (u *User) LastMsg() time.Time {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastMsg
}

// SetAway sets the users away reason and state.
func (u *User) SetAway(msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.awayReason = msg
	if msg == "" {
		u.awaySince = time.Time{}
	} else {
		// Reset away timer even if already away
		u.awaySince = time.Now()
	}
}

// GetAway returns if the user is away, when they went away, and the reason.
func (u *User) GetAway() (bool, time.Time, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.awayReason != "", u.awaySince, u.awayReason
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

	if u.OnChange != nil {
		u.OnChange()
	}
}

// Rename the user with a new Identifier.
func (u *User) SetID(id string) {
	u.Identifier.SetID(id)
	u.setColorIdx(rand.Int())

	if u.OnChange != nil {
		u.OnChange()
	}
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
	var out string
	switch m := m.(type) {
	case PublicMsg:
		if u == m.From() {
			u.mu.Lock()
			u.lastMsg = m.Timestamp()
			u.mu.Unlock()

			if !cfg.Echo {
				return ""
			}
			out += m.RenderSelf(cfg)
		} else if u.Focused.Len() > 0 && !u.Focused.In(m.From().ID()) {
			// Skip message during focus
			return ""
		} else {
			out += m.RenderFor(cfg)
		}
	case *PrivateMsg:
		out += m.Render(cfg.Theme)
		if cfg.Bell {
			out += Bel
		}
	case *CommandMsg:
		out += m.RenderSelf(cfg)
	default:
		out += m.Render(cfg.Theme)
	}
	if cfg.Timeformat != nil {
		ts := m.Timestamp()
		if cfg.Timezone != nil {
			ts = ts.In(cfg.Timezone)
		} else {
			ts = ts.UTC()
		}
		return cfg.Theme.Timestamp(ts.Format(*cfg.Timeformat)) + "  " + out + Newline
	}
	return out + Newline
}

// writeMsg renders the message and attempts to write it, will Close the user
// if it fails.
func (u *User) writeMsg(m Message) error {
	r := u.render(m)
	_, err := u.screen.Write([]byte(r))
	if err != nil {
		logger.Printf("Write failed to %s, closing: %s", u.ID(), err)
		u.Close()
	}
	return err
}

// HandleMsg will render the message to the screen, blocking.
func (u *User) HandleMsg(m Message) error {
	return u.writeMsg(m)
}

// Add message to consume by user
func (u *User) Send(m Message) error {
	select {
	case <-u.done:
		return ErrUserClosed
	case u.msg <- m:
	case <-time.After(messageTimeout):
		logger.Printf("Message buffer full, closing: %s", u.ID())
		u.Close()
		return ErrUserClosed
	}
	return nil
}

// Container for per-user configurations.
type UserConfig struct {
	Highlight  *regexp.Regexp
	Bell       bool
	Quiet      bool
	Echo       bool // Echo shows your own messages after sending, disabled for bots
	Timeformat *string
	Timezone   *time.Location
	Theme      *Theme
}

// Default user configuration to use
var DefaultUserConfig UserConfig

func init() {
	DefaultUserConfig = UserConfig{
		Bell:  true,
		Echo:  true,
		Quiet: false,
	}

	// TODO: Seed random?
}

// RecentActiveUsers is a slice of *Users that knows how to be sorted by the
// time of the last message. If no message has been sent, then fall back to the
// time joined instead.
type RecentActiveUsers []*User

func (a RecentActiveUsers) Len() int      { return len(a) }
func (a RecentActiveUsers) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a RecentActiveUsers) Less(i, j int) bool {
	a[i].mu.Lock()
	defer a[i].mu.Unlock()
	a[j].mu.Lock()
	defer a[j].mu.Unlock()

	ai := a[i].lastMsg
	if ai.IsZero() {
		ai = a[i].joined
	}

	aj := a[j].lastMsg
	if aj.IsZero() {
		aj = a[j].joined
	}

	return ai.After(aj)
}
