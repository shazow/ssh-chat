package sshchat

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/shazow/ssh-chat/set"
	"github.com/shazow/ssh-chat/sshd"
	"golang.org/x/crypto/ssh"
)

// ErrNotWhitelisted Is the error returned when a key is checked that is not whitelisted,
// when whitelisting is enabled.
var ErrNotWhitelisted = errors.New("not whitelisted")

// ErrBanned is the error returned when a key is checked that is banned.
var ErrBanned = errors.New("banned")

// newAuthKey returns string from an ssh.PublicKey used to index the key in our lookup.
func newAuthKey(key ssh.PublicKey) string {
	if key == nil {
		return ""
	}
	// FIXME: Is there a better way to index pubkeys without marshal'ing them into strings?
	return sshd.Fingerprint(key)
}

func newAuthItem(key ssh.PublicKey) set.Item {
	return set.StringItem(newAuthKey(key))
}

// newAuthAddr returns a string from a net.Addr used to index the address the key in our lookup.
func newAuthAddr(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, _ := net.SplitHostPort(addr.String())
	return host
}

// Auth stores lookups for bans, whitelists, and ops. It implements the sshd.Auth interface.
// If the contained password is not empty, it complements a whitelist.
type Auth struct {
	passwordHash []byte
	bannedAddr   *set.Set
	bannedClient *set.Set
	banned       *set.Set
	whitelist    *set.Set
	ops          *set.Set
}

// NewAuth creates a Auth, optionally with a password.
func NewAuth() *Auth {
	return &Auth{
		bannedAddr:   set.New(),
		bannedClient: set.New(),
		banned:       set.New(),
		whitelist:    set.New(),
		ops:          set.New(),
	}
}

// SetPassword anables password authentication with the given password.
// If an emty password is geiven, disable password authentication.
func (a *Auth) SetPassword(password string) {
	if password == "" {
		a.passwordHash = nil
	} else {
		hashArray := sha256.Sum256([]byte(password))
		a.passwordHash = hashArray[:]
	}
}

// AllowAnonymous determines if anonymous users are permitted.
func (a *Auth) AllowAnonymous() bool {
	return a.whitelist.Len() == 0 && a.passwordHash == nil
}

// AcceptPassword determines if password authentication is accepted.
func (a *Auth) AcceptPassword() bool {
	return a.passwordHash != nil
}

// Check determines if a pubkey fingerprint is permitted.
func (a *Auth) Check(addr net.Addr, key ssh.PublicKey, clientVersion string) error {
	authkey := newAuthKey(key)

	if !a.AllowAnonymous() {
		// Only check whitelist if we don't allow everyone to connect.
		whitelisted := a.whitelist.In(authkey)
		if !whitelisted {
			return ErrNotWhitelisted
		}
		return nil
	}

	var banned bool
	if authkey != "" {
		banned = a.banned.In(authkey)
	}
	if !banned {
		banned = a.bannedAddr.In(newAuthAddr(addr))
	}
	if !banned {
		banned = a.bannedClient.In(clientVersion)
	}
	// Ops can bypass bans, just in case we ban ourselves.
	if banned && !a.IsOp(key) {
		return ErrBanned
	}

	return nil
}

// CheckPassword determines if a password is permitted.
func (a *Auth) CheckPassword(password string) error {
	if !a.AcceptPassword() {
		return errors.New("passwords not accepted") // this should never happen
	}
	passedPasswordhash := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(passedPasswordhash[:], a.passwordHash) == 0 {
		return errors.New("incorrect password")
	}
	return nil
}

// Op sets a public key as a known operator.
func (a *Auth) Op(key ssh.PublicKey, d time.Duration) {
	if key == nil {
		return
	}
	authItem := newAuthItem(key)
	if d != 0 {
		a.ops.Set(set.Expire(authItem, d))
	} else {
		a.ops.Set(authItem)
	}
	logger.Debugf("Added to ops: %q (for %s)", authItem.Key(), d)
}

// IsOp checks if a public key is an op.
func (a *Auth) IsOp(key ssh.PublicKey) bool {
	if key == nil {
		return false
	}
	authkey := newAuthKey(key)
	return a.ops.In(authkey)
}

// Whitelist will set a public key as a whitelisted user.
func (a *Auth) Whitelist(key ssh.PublicKey, d time.Duration) {
	if key == nil {
		return
	}
	authItem := newAuthItem(key)
	if d != 0 {
		a.whitelist.Set(set.Expire(authItem, d))
	} else {
		a.whitelist.Set(authItem)
	}
	logger.Debugf("Added to whitelist: %q (for %s)", authItem.Key(), d)
}

// Ban will set a public key as banned.
func (a *Auth) Ban(key ssh.PublicKey, d time.Duration) {
	if key == nil {
		return
	}
	a.BanFingerprint(newAuthKey(key), d)
}

// BanFingerprint will set a public key fingerprint as banned.
func (a *Auth) BanFingerprint(authkey string, d time.Duration) {
	// FIXME: This is a case insensitive key, which isn't great...
	authItem := set.StringItem(authkey)
	if d != 0 {
		a.banned.Set(set.Expire(authItem, d))
	} else {
		a.banned.Set(authItem)
	}
	logger.Debugf("Added to banned: %q (for %s)", authItem.Key(), d)
}

// BanClient will set client version as banned. Useful for misbehaving bots.
func (a *Auth) BanClient(client string, d time.Duration) {
	item := set.StringItem(client)
	if d != 0 {
		a.bannedClient.Set(set.Expire(item, d))
	} else {
		a.bannedClient.Set(item)
	}
	logger.Debugf("Added to banned: %q (for %s)", item.Key(), d)
}

// Banned returns the list of banned keys.
func (a *Auth) Banned() (ip []string, fingerprint []string, client []string) {
	a.banned.Each(func(key string, _ set.Item) error {
		fingerprint = append(fingerprint, key)
		return nil
	})
	a.bannedAddr.Each(func(key string, _ set.Item) error {
		ip = append(ip, key)
		return nil
	})
	a.bannedClient.Each(func(key string, _ set.Item) error {
		client = append(client, key)
		return nil
	})
	return
}

// BanAddr will set an IP address as banned.
func (a *Auth) BanAddr(addr net.Addr, d time.Duration) {
	authItem := set.StringItem(newAuthAddr(addr))
	if d != 0 {
		a.bannedAddr.Set(set.Expire(authItem, d))
	} else {
		a.bannedAddr.Set(authItem)
	}
	logger.Debugf("Added to bannedAddr: %q (for %s)", authItem.Key(), d)
}

// BanQuery takes space-separated key="value" pairs to ban, including ip, fingerprint, client.
// Fields without an = will be treated as a duration, applied to the next field.
// For example: 5s client=foo 10min ip=1.1.1.1
// Will ban client foo for 5 seconds, and ip 1.1.1.1 for 10min.
func (a *Auth) BanQuery(q string) error {
	r := csv.NewReader(strings.NewReader(q))
	r.Comma = ' '
	fields, err := r.Read()
	if err != nil {
		return err
	}

	var d time.Duration
	if last := fields[len(fields)-1]; !strings.Contains(last, "=") {
		d, err = time.ParseDuration(last)
		if err != nil {
			return err
		}
		fields = fields[:len(fields)-1]
	}
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid query: %q", q)
		}
		key, value := parts[0], parts[1]
		switch key {
		case "client":
			a.BanClient(value, d)
		case "fingerprint":
			// TODO: Add a validity check?
			a.BanFingerprint(value, d)
		case "ip":
			ip := net.ParseIP(value)
			if ip.String() == "" {
				return fmt.Errorf("invalid ip value: %q", ip)
			}
			a.BanAddr(&net.TCPAddr{IP: ip}, d)
		default:
			return fmt.Errorf("unknown query field: %q", field)
		}
	}

	return nil
}
