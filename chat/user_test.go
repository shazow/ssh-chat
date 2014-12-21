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
	u := NewUser("foo", s)

	line := []byte("hello")
	u.Write(line)
	if !reflect.DeepEqual(s.received, line) {
		t.Errorf("Expected hello but got: %s", s.received)
	}
}
