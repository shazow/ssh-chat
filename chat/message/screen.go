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

var ErrUserClosed = errors.New("user closed")

const messageBuffer = 5
const messageTimeout = 5 * time.Second

func BufferedScreen(name string, screen io.WriteCloser) *bufferedScreen {
	return &bufferedScreen{
		pipedScreen: PipedScreen(name, screen),
		msg:         make(chan Message, messageBuffer),
		done:        make(chan struct{}),
	}
}

func PipedScreen(name string, screen io.WriteCloser) *pipedScreen {
	return &pipedScreen{
		baseScreen:  Screen(name),
		WriteCloser: screen,
	}
}

func HandledScreen(name string, handler func(Message) error) *handledScreen {
	return &handledScreen{
		baseScreen: Screen(name),
		handler:    handler,
	}
}

func Screen(name string) *baseScreen {
	return &baseScreen{
		User: NewUser(name),
	}
}

type handledScreen struct {
	*baseScreen
	handler func(Message) error
}

func (u *handledScreen) Send(m Message) error {
	return u.handler(m)
}

// Screen that pipes messages to an io.WriteCloser
type pipedScreen struct {
	*baseScreen
	io.WriteCloser
}

func (u *pipedScreen) Send(m Message) error {
	r := u.render(m)
	_, err := u.Write([]byte(r))
	if err != nil {
		logger.Printf("Write failed to %s, closing: %s", u.Name(), err)
		u.Close()
	}
	return err
}

// User container that knows about writing to an IO screen.
type baseScreen struct {
	sync.Mutex
	*User
}

func (u *baseScreen) Config() UserConfig {
	u.Lock()
	defer u.Unlock()
	return u.config
}

func (u *baseScreen) SetConfig(cfg UserConfig) {
	u.Lock()
	u.config = cfg
	u.Unlock()
}

func (u *baseScreen) ID() string {
	u.Lock()
	defer u.Unlock()
	return SanitizeName(u.name)
}

func (u *baseScreen) Name() string {
	u.Lock()
	defer u.Unlock()
	return u.name
}

func (u *baseScreen) Joined() time.Time {
	return u.joined
}

// Rename the user with a new Identifier.
func (u *baseScreen) SetName(name string) {
	u.Lock()
	u.name = name
	u.config.Seed = rand.Int()
	u.Unlock()
}

// ReplyTo returns the last user that messaged this user.
func (u *baseScreen) ReplyTo() Author {
	u.Lock()
	defer u.Unlock()
	return u.replyTo
}

// SetReplyTo sets the last user to message this user.
func (u *baseScreen) SetReplyTo(user Author) {
	// TODO: Use UserConfig.ReplyTo string
	u.Lock()
	defer u.Unlock()
	u.replyTo = user
}

// SetHighlight sets the highlighting regular expression to match string.
func (u *baseScreen) SetHighlight(s string) error {
	re, err := regexp.Compile(fmt.Sprintf(reHighlight, s))
	if err != nil {
		return err
	}
	u.Lock()
	u.config.Highlight = re
	u.Unlock()
	return nil
}

func (u *baseScreen) render(m Message) string {
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

// Prompt renders a theme-colorized prompt string.
func (u *baseScreen) Prompt() string {
	name := u.Name()
	cfg := u.Config()
	if cfg.Theme != nil {
		name = cfg.Theme.ColorName(u)
	}
	return fmt.Sprintf("[%s] ", name)
}

// bufferedScreen is a screen that buffers messages on Send using a channel and a consuming goroutine.
type bufferedScreen struct {
	*pipedScreen
	closeOnce sync.Once
	msg       chan Message
	done      chan struct{}
}

func (u *bufferedScreen) Close() error {
	u.closeOnce.Do(func() {
		close(u.done)
	})

	return u.pipedScreen.Close()
}

// Add message to consume by user
func (u *bufferedScreen) Send(m Message) error {
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

// Consume message buffer into the handler. Will block, should be called in a
// goroutine.
func (u *bufferedScreen) Consume() {
	for {
		select {
		case <-u.done:
			return
		case m, ok := <-u.msg:
			if !ok {
				return
			}
			// Pass on to unbuffered screen.
			u.pipedScreen.Send(m)
		}
	}
}
