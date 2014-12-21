package chat

import "testing"

func TestMessage(t *testing.T) {
	var expected, actual string

	expected = " * foo"
	actual = NewMessage("foo").String()
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	u := NewUser("foo")
	expected = "foo: hello"
	actual = NewMessage("hello").From(u).String()
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	expected = "-> hello"
	actual = NewMessage("hello").To(u).String()
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	expected = "[PM from foo] hello"
	actual = NewMessage("hello").From(u).To(u).String()
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}

// TODO: Add theme rendering tests
