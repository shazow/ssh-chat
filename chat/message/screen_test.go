package message

import (
	"reflect"
	"testing"
)

// Used for testing
type MockScreen struct {
	buffer []byte
}

func (s *MockScreen) Write(data []byte) (n int, err error) {
	s.buffer = append(s.buffer, data...)
	return len(data), nil
}

func (s *MockScreen) Read(p *[]byte) (n int, err error) {
	*p = s.buffer
	s.buffer = []byte{}
	return len(*p), nil
}

func TestScreen(t *testing.T) {
	var actual, expected []byte

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %v; Expected: %v", actual, expected)
	}

	actual = []byte("foo")
	expected = []byte("foo")
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %v; Expected: %v", actual, expected)
	}

	s := &MockScreen{}

	expected = nil
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %v; Expected: %v", actual, expected)
	}

	expected = []byte("hello, world")
	s.Write(expected)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %v; Expected: %v", actual, expected)
	}
}
