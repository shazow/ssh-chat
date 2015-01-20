package message

import "testing"

func msgEqual(a []Message, b []Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].String() != b[i].String() {
			return false
		}
	}
	return true
}

func TestHistory(t *testing.T) {
	var r, expected []Message
	var size int

	h := NewHistory(5)

	r = h.Get(10)
	expected = []Message{}
	if !msgEqual(r, expected) {
		t.Errorf("Got: %v, Expected: %v", r, expected)
	}

	h.Add(NewMsg("1"))

	if size = h.Len(); size != 1 {
		t.Errorf("Wrong size: %v", size)
	}

	r = h.Get(1)
	expected = []Message{NewMsg("1")}
	if !msgEqual(r, expected) {
		t.Errorf("Got: %v, Expected: %v", r, expected)
	}

	h.Add(NewMsg("2"))
	h.Add(NewMsg("3"))
	h.Add(NewMsg("4"))
	h.Add(NewMsg("5"))
	h.Add(NewMsg("6"))

	if size = h.Len(); size != 5 {
		t.Errorf("Wrong size: %v", size)
	}

	r = h.Get(2)
	expected = []Message{NewMsg("5"), NewMsg("6")}
	if !msgEqual(r, expected) {
		t.Errorf("Got: %v, Expected: %v", r, expected)
	}

	r = h.Get(10)
	expected = []Message{NewMsg("2"), NewMsg("3"), NewMsg("4"), NewMsg("5"), NewMsg("6")}
	if !msgEqual(r, expected) {
		t.Errorf("Got: %v, Expected: %v", r, expected)
	}
}
