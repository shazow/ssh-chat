package chat

import (
	"reflect"
	"testing"
)

type MockScreen struct {
	received []byte
}

func (s *MockScreen) Write(data []byte) (n int, err error) {
	s.received = append(s.received, data...)
	return len(data), nil
}

func TestMakeUser(t *testing.T) {
	s := &MockScreen{}
	u := NewUser("foo")
	m := NewMessage("hello")

	defer u.Close()
	u.Send(*m)
	u.ConsumeOne(s)

	if !reflect.DeepEqual(string(s.received), m.String()) {
		t.Errorf("Got: `%s`; Expected: `%s`", s.received, m.String())
	}
}
