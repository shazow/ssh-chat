package main

import (
	"testing"

	"github.com/shazow/ssh-chat/chat"
)

func TestHostGetPrompt(t *testing.T) {
	var expected, actual string

	u := chat.NewUser("foo")
	u.SetColorIdx(2)

	actual = GetPrompt(u)
	expected = "[foo] "
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	u.Config.Theme = &chat.Themes[0]
	actual = GetPrompt(u)
	expected = "[\033[38;05;2mfoo\033[0m] "
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}
