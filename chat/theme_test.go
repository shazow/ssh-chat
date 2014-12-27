package chat

import (
	"fmt"
	"testing"
)

func TestThemePalette(t *testing.T) {
	var expected, actual string

	palette := readableColors256()
	color := palette.Get(5)
	if color == nil {
		t.Fatal("Failed to return a color from palette.")
	}

	actual = color.String()
	expected = "38;05;5"
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	actual = color.Format("foo")
	expected = "\033[38;05;5mfoo\033[0m"
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	actual = palette.Get(palette.Len() + 1).String()
	expected = fmt.Sprintf("38;05;%d", 2)
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

}

func TestTheme(t *testing.T) {
	var expected, actual string

	colorTheme := Themes[0]
	color := colorTheme.sys
	if color == nil {
		t.Fatal("Sys color should not be empty for first theme.")
	}

	actual = color.Format("foo")
	expected = "\033[38;05;8mfoo\033[0m"
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	actual = colorTheme.ColorSys("foo")
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	u := NewUser("foo")
	u.colorIdx = 4
	actual = colorTheme.ColorName(u)
	expected = "\033[38;05;4mfoo\033[0m"
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	msg := NewPublicMsg("hello", u)
	actual = msg.Render(&colorTheme)
	expected = "\033[38;05;4mfoo\033[0m: hello"
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}
