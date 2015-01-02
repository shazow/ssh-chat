package main

import (
	"errors"
	"sync"

	"github.com/shazow/ssh-chat/sshd"
)

// Auth stores fingerprint lookups
type Auth struct {
	whitelist map[string]struct{}
	banned    map[string]struct{}
	ops       map[string]struct{}

	sshd.Auth
	sync.RWMutex
}

// AllowAnonymous determines if anonymous users are permitted.
func (a Auth) AllowAnonymous() bool {
	a.RLock()
	ok := len(a.whitelist) == 0
	a.RUnlock()
	return ok
}

// Check determines if a pubkey fingerprint is permitted.
func (a Auth) Check(fingerprint string) (bool, error) {
	a.RLock()
	defer a.RUnlock()

	if len(a.whitelist) > 0 {
		// Only check whitelist if there is something in it, otherwise it's disabled.
		_, whitelisted := a.whitelist[fingerprint]
		if !whitelisted {
			return false, errors.New("not whitelisted")
		}
	}

	_, banned := a.banned[fingerprint]
	if banned {
		return false, errors.New("banned")
	}

	return true, nil
}

// Op will set a fingerprint as a known operator.
func (a *Auth) Op(fingerprint string) {
	a.Lock()
	a.ops[fingerprint] = struct{}{}
	a.Unlock()
}

// Whitelist will set a fingerprint as a whitelisted user.
func (a *Auth) Whitelist(fingerprint string) {
	a.Lock()
	a.whitelist[fingerprint] = struct{}{}
	a.Unlock()
}

// Ban will set a fingerprint as banned.
func (a *Auth) Ban(fingerprint string) {
	a.Lock()
	a.banned[fingerprint] = struct{}{}
	a.Unlock()
}
