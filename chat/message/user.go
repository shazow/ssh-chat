package message

import (
	"math/rand"
	"time"
)

const reHighlight = `\b(%s)\b`

// User definition, implemented set Item interface and io.Writer
type user struct {
	joined time.Time

	name    string
	config  UserConfig
	replyTo Author // Set when user gets a /msg, for replying.
}

func NewUser(name string) *user {
	u := user{
		name:   name,
		config: DefaultUserConfig,
		joined: time.Now(),
	}
	u.config.Seed = rand.Int()

	return &u
}

func (u *user) Name() string {
	return u.name
}

func (u *user) Color() int {
	return u.config.Seed
}

func (u *user) ID() string {
	return SanitizeName(u.name)
}

func (u *user) Joined() time.Time {
	return u.joined
}
