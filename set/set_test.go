package set

import (
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

	item := &ExpiringItem{nil, time.Now().Add(-time.Nanosecond * 1)}
	if !item.Expired() {
		t.Errorf("ExpiringItem a nanosec ago is not expiring")
	}

	item = &ExpiringItem{nil, time.Now().Add(time.Minute * 2)}
	if item.Expired() {
		t.Errorf("ExpiringItem in 2 minutes is expiring now")
	}

	item = Expire(StringItem("bar"), time.Minute*2).(*ExpiringItem)
	until := item.Time
	if !until.After(time.Now().Add(time.Minute*1)) || !until.Before(time.Now().Add(time.Minute*3)) {
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

	if err := s.Replace("bar", Expire(StringItem("bar"), time.Minute*5)); err != nil {
		t.Fatalf("failed to add bar: %s", err)
	}
	if !s.In("bar") {
		t.Error("failed to match before expiry")
	}
}
