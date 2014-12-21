package chat

import (
	"reflect"
	"testing"
)

func TestMakeUser(t *testing.T) {
	var actual, expected []byte

	s := &MockScreen{}
	u := NewUser("foo")
	m := NewMessage("hello")

	defer u.Close()
	u.Send(*m)
	u.ConsumeOne(s)

	s.Read(&actual)
	expected = []byte(m.String())
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}
