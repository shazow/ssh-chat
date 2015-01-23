package sshchat

import (
	"testing"
	"time"
)

func TestSetExpiring(t *testing.T) {
	s := NewSet()
	if s.In("foo") {
		t.Error("Matched before set.")
	}

	s.Add("foo")
	if !s.In("foo") {
		t.Errorf("Not matched after set")
	}
	if s.Len() != 1 {
		t.Error("Not len 1 after set")
	}

	v := expiringValue{time.Now().Add(-time.Nanosecond * 1)}
	if v.Bool() {
		t.Errorf("expiringValue now is not expiring")
	}

	v = expiringValue{time.Now().Add(time.Minute * 2)}
	if !v.Bool() {
		t.Errorf("expiringValue in 2 minutes is expiring now")
	}

	until := s.AddExpiring("bar", time.Minute*2)
	if !until.After(time.Now().Add(time.Minute*1)) || !until.Before(time.Now().Add(time.Minute*3)) {
		t.Errorf("until is not a minute after %s: %s", time.Now(), until)
	}
	val, ok := s.lookup["bar"]
	if !ok {
		t.Errorf("bar not in lookup")
	}
	if !until.Equal(val.(expiringValue).Time) {
		t.Errorf("bar's until is not equal to the expected value")
	}
	if !val.Bool() {
		t.Errorf("bar expired immediately")
	}

	if !s.In("bar") {
		t.Errorf("Not matched after timed set")
	}
	if s.Len() != 2 {
		t.Error("Not len 2 after set")
	}

	s.AddExpiring("bar", time.Nanosecond*1)
	if s.In("bar") {
		t.Error("Matched after expired timer")
	}
}
