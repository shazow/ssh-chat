package set

import (
	"strings"
	"testing"
	"time"
)

func TestSetExpiring(t *testing.T) {
	s := New()
	if s.In("foo") {
		t.Error("matched before set.")
	}

	if err := s.Add(StringItem("foo")); err != nil {
		t.Fatalf("failed to add foo: %s", err)
	}
	if !s.In("foo") {
		t.Errorf("not matched after set")
	}
	if s.Len() != 1 {
		t.Error("not len 1 after set")
	}

	item := &ExpiringItem{StringItem("expired"), time.Now().Add(-time.Nanosecond * 1)}
	if !item.Expired() {
		t.Errorf("ExpiringItem a nanosec ago is not expiring")
	}
	if err := s.Add(item); err != nil {
		t.Fatalf("failed to add item: %s", err)
	}
	if s.In("expired") {
		t.Errorf("expired item is present")
	}

	item = &ExpiringItem{nil, time.Now().Add(time.Minute * 5)}
	if item.Expired() {
		t.Errorf("ExpiringItem in 2 minutes is expiring now")
	}

	item = Expire(StringItem("bar"), time.Minute*5).(*ExpiringItem)
	until := item.Time
	if !until.After(time.Now().Add(time.Minute*4)) || !until.Before(time.Now().Add(time.Minute*6)) {
		t.Errorf("until is not a minute after %s: %s", time.Now(), until)
	}
	if item.Value() == nil {
		t.Errorf("bar expired immediately")
	}
	if err := s.Add(item); err != nil {
		t.Fatalf("failed to add item: %s", err)
	}
	_, ok := s.lookup["bar"]
	if !ok {
		t.Fatalf("expired bar added to lookup")
	}
	s.lookup["bar"] = item

	if !s.In("bar") {
		t.Errorf("not matched after timed set")
	}
	if s.Len() != 2 {
		t.Error("not len 2 after set")
	}
	if err := s.Replace("bar", Expire(StringItem("quux"), time.Minute*5)); err != nil {
		t.Fatalf("failed to add quux: %s", err)
	}

	if err := s.Replace("quux", Expire(StringItem("bar"), time.Minute*5)); err != nil {
		t.Fatalf("failed to add bar: %s", err)
	}
	if s.In("quux") {
		t.Error("quux in set after replace")
	}
	if _, err := s.Get("bar"); err != nil {
		t.Errorf("failed to get before expiry: %s", err)
	}
	if err := s.Add(StringItem("barbar")); err != nil {
		t.Fatalf("failed to add barbar")
	}
	if _, err := s.Get("barbar"); err != nil {
		t.Errorf("failed to get barbar: %s", err)
	}
	b := s.ListPrefix("b")
	if len(b) != 2 {
		t.Errorf("b-prefix incorrect number of results: %d", len(b))
	}
	for i, item := range b {
		if !strings.HasPrefix(item.Key(), "b") {
			t.Errorf("item %d does not have b prefix: %s", i, item.Key())
		}
	}

	if err := s.Remove("bar"); err != nil {
		t.Fatalf("failed to remove: %s", err)
	}
	if s.Len() != 2 {
		t.Error("not len 2 after remove")
	}
	s.Clear()
	if s.Len() != 0 {
		t.Error("not len 0 after clear")
	}
}
