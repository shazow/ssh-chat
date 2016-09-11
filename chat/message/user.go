package message

import (
	"math/rand"
	"time"
)

const reHighlight = `\b(%s)\b`

// User definition, implemented set Item interface and io.Writer
type User struct {
	joined time.Time

	name    string
	config  UserConfig
	replyTo Author // Set when user gets a /msg, for replying.
}

func NewUser(name string) *User {
	u := User{
		name:   name,
		config: DefaultUserConfig,
		joined: time.Now(),
	}
	u.config.Seed = rand.Int()

	return &u
}

func (u *User) Name() string {
	return u.name
}

func (u *User) Color() int {
	return u.config.Seed
}

func (u *User) ID() string {
	return SanitizeName(u.name)
}
