package message

import (
	"reflect"
	"testing"
)

func TestMakeUser(t *testing.T) {
	var actual, expected []byte

	s := &MockScreen{}
	u := NewUser(SimpleId("foo"))
	m := NewAnnounceMsg("hello")

	defer u.Close()
	u.Send(m)
	u.HandleMsg(<-u.ConsumeChan(), s)

	s.Read(&actual)
	expected = []byte(m.String() + Newline)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}
