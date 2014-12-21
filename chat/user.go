package chat

import (
	"io"
	"math/rand"
	"time"
)

// User definition, implemented set Item interface and io.Writer
type User struct {
	name     string
	op       bool
	colorIdx int
	joined   time.Time
	screen   io.Writer
	Config   UserConfig
}

func NewUser(name string, screen io.Writer) *User {
	u := User{
		screen: screen,
		joined: time.Now(),
		Config: *DefaultUserConfig,
	}
	u.SetName(name)
	return &u
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

// Write to user's screen
func (u *User) Write(p []byte) (n int, err error) {
	return u.screen.Write(p)
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
		Theme:     DefaultTheme,
	}

	// TODO: Seed random?
}
