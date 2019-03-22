package message

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestMakeUser(t *testing.T) {
	var actual, expected []byte

	s := &MockScreen{}
	u := NewUserScreen(SimpleID("foo"), s)

	cfg := u.Config()
	cfg.Theme = MonoTheme // Mono
	u.SetConfig(cfg)

	m := NewAnnounceMsg("hello")

	defer u.Close()
	u.Send(m)
	u.HandleMsg(u.ConsumeOne())

	s.Read(&actual)
	expected = []byte(m.String() + Newline)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}

func TestRenderTimestamp(t *testing.T) {
	var actual, expected []byte

	// Reset seed for username color
	rand.Seed(1)
	s := &MockScreen{}
	u := NewUserScreen(SimpleID("foo"), s)

	cfg := u.Config()
	timefmt := "AA:BB"
	cfg.Theme = DefaultTheme
	cfg.Timeformat = &timefmt
	u.SetConfig(cfg)

	if got, want := cfg.Theme.Timestamp("foo"), `[38;05;245mfoo`+Reset; got != want {
		t.Errorf("Wrong timestamp formatting:\n got: %q\nwant: %q", got, want)
	}

	m := NewPublicMsg("hello", u)

	defer u.Close()
	u.Send(m)
	u.HandleMsg(u.ConsumeOne())

	s.Read(&actual)
	expected = []byte(`[38;05;245mAA:BB` + Reset + `  [[38;05;88mfoo[0m] hello` + Newline)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Wrong screen output:\n Got: `%q`;\nWant: `%q`", actual, expected)
	}
}
