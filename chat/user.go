package chat

import (
	"errors"
	"io"
	"math/rand"
	"time"
)

const messageBuffer = 20

var ErrUserClosed = errors.New("user closed")

// User definition, implemented set Item interface and io.Writer
type User struct {
	name     string
	op       bool
	colorIdx int
	joined   time.Time
	msg      chan Message
	done     chan struct{}
	Config   UserConfig
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

// Return unique identifier for user
func (u *User) Id() Id {
	return Id(u.name)
}

// Return user's name
func (u *User) Name() string {
	return u.name
}

// Return set user's name
func (u *User) SetName(name string) {
	u.name = name
	u.colorIdx = rand.Int()
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
	close(u.done)
	close(u.msg)
}

// Consume message buffer into an io.Writer. Will block, should be called in a
// goroutine.
// TODO: Not sure if this is a great API.
func (u *User) Consume(out io.Writer) {
	for m := range u.msg {
		u.consumeMsg(m, out)
	}
}

// Consume one message and stop, mostly for testing
func (u *User) ConsumeOne(out io.Writer) {
	u.consumeMsg(<-u.msg, out)
}

func (u *User) consumeMsg(m Message, out io.Writer) {
	s := m.Render(u.Config.Theme)
	_, err := out.Write([]byte(s))
	if err != nil {
		logger.Printf("Write failed to %s, closing: %s", u.Name(), err)
		u.Close()
	}
}

// Add message to consume by user
func (u *User) Send(m Message) error {
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
