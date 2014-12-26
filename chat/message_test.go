package chat

import "testing"

func TestMessage(t *testing.T) {
	var expected, actual string

	expected = " * foo"
	actual = NewAnnounceMsg("foo").String()
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	u := NewUser("foo")
	expected = "foo: hello"
	actual = NewPublicMsg("hello", u).String()
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	expected = "-> hello"
	actual = NewSystemMsg("hello", u).String()
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	expected = "[PM from foo] hello"
	actual = NewPrivateMsg("hello", u, u).String()
	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}

// TODO: Add theme rendering tests
