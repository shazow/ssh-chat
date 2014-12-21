package chat

import "testing"

func equal(a []interface{}, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		if a[0] != b[0] {
			return false
		}
	}

	return true
}

func TestHistory(t *testing.T) {
	var r []interface{}
	var expected []string
	var size int

	h := NewHistory(5)

	r = h.Get(10)
	expected = []string{}
	if !equal(r, expected) {
		t.Errorf("Got: %v, Expected: %v", r, expected)
	}

	h.Add("1")

	if size = h.Len(); size != 1 {
		t.Errorf("Wrong size: %v", size)
	}

	r = h.Get(1)
	expected = []string{"1"}
	if !equal(r, expected) {
		t.Errorf("Got: %v, Expected: %v", r, expected)
	}

	h.Add("2")
	h.Add("3")
	h.Add("4")
	h.Add("5")
	h.Add("6")

	if size = h.Len(); size != 5 {
		t.Errorf("Wrong size: %v", size)
	}

	r = h.Get(2)
	expected = []string{"5", "6"}
	if !equal(r, expected) {
		t.Errorf("Got: %v, Expected: %v", r, expected)
	}

	r = h.Get(10)
	expected = []string{"2", "3", "4", "5", "6"}
	if !equal(r, expected) {
		t.Errorf("Got: %v, Expected: %v", r, expected)
	}
}
