package chat

import (
	"errors"
	"io"
	"math/rand"
	"sync"
	"time"
)

const messageBuffer = 20

var ErrUserClosed = errors.New("user closed")

// User definition, implemented set Item interface and io.Writer
type User struct {
	Config    UserConfig
	name      string
	op        bool
	colorIdx  int
	joined    time.Time
	msg       chan Message
	done      chan struct{}
	closed    bool
	closeOnce sync.Once
}

func NewUser(name string) *User {
	u := User{
		Config: *DefaultUserConfig,
		joined: time.Now(),
		msg:    make(chan Message, messageBuffer),
		done:   make(chan struct{}, 1),
	}
	u.SetName(name)

	return &u
}

func NewUserScreen(name string, screen io.Writer) *User {
	u := NewUser(name)
	go u.Consume(screen)

	return u
}

// Id of the user, a unique identifier within a set
func (u *User) Id() Id {
	return Id(u.name)
}

// Name of the user
func (u *User) Name() string {
	return u.name
}

// SetName will change the name of the user and reset the colorIdx
func (u *User) SetName(name string) {
	u.name = name
	u.SetColorIdx(rand.Int())
}

// SetColorIdx will set the colorIdx to a specific value, primarily used for
// testing.
func (u *User) SetColorIdx(idx int) {
	u.colorIdx = idx
}

// Return whether user is an admin
func (u *User) Op() bool {
	return u.op
}

// Set whether user is an admin
func (u *User) SetOp(op bool) {
	u.op = op
}

// Block until user is closed
func (u *User) Wait() {
	<-u.done
}

// Disconnect user, stop accepting messages
func (u *User) Close() {
	u.closeOnce.Do(func() {
		u.closed = true
		close(u.done)
		close(u.msg)
	})
}

// Consume message buffer into an io.Writer. Will block, should be called in a
// goroutine.
// TODO: Not sure if this is a great API.
func (u *User) Consume(out io.Writer) {
	for m := range u.msg {
		u.HandleMsg(m, out)
	}
}

// Consume one message and stop, mostly for testing
func (u *User) ConsumeOne(out io.Writer) {
	u.HandleMsg(<-u.msg, out)
}

func (u *User) HandleMsg(m Message, out io.Writer) {
	s := m.Render(u.Config.Theme)
	_, err := out.Write([]byte(s + Newline))
	if err != nil {
		logger.Printf("Write failed to %s, closing: %s", u.Name(), err)
		u.Close()
	}
}

// Add message to consume by user
func (u *User) Send(m Message) error {
	if u.closed {
		return ErrUserClosed
	}

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
	Highlight bool
	Bell      bool
	Theme     *Theme
}

// Default user configuration to use
var DefaultUserConfig *UserConfig

func init() {
	DefaultUserConfig = &UserConfig{
		Highlight: true,
		Bell:      false,
	}

	// TODO: Seed random?
}
