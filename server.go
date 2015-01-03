package main

import (
	"bufio"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	maxNameLength        = 32
	historyLength        = 20
	systemMessageFormat  = "\033[1;90m"
	privateMessageFormat = "\033[1m"
	highlightFormat      = Bold + "\033[48;5;11m\033[38;5;16m"
	beep                 = "\007"
)

var (
	reStripText = regexp.MustCompile("[^0-9A-Za-z_.-]")
)

// Clients is a map of clients
type Clients map[string]*Client

// Server holds all the fields used by a server
type Server struct {
	sshConfig *ssh.ServerConfig
	done      chan struct{}
	clients   Clients
	count     int
	history   *History
	motd      string
	whitelist map[string]struct{}   // fingerprint lookup
	admins    map[string]struct{}   // fingerprint lookup
	bannedPK  map[string]*time.Time // fingerprint lookup
	started   time.Time
	sync.RWMutex
}

// NewServer constructs a new server
func NewServer(privateKey []byte) (*Server, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	server := Server{
		done:      make(chan struct{}),
		clients:   Clients{},
		count:     0,
		history:   NewHistory(historyLength),
		motd:      "",
		whitelist: map[string]struct{}{},
		admins:    map[string]struct{}{},
		bannedPK:  map[string]*time.Time{},
		started:   time.Now(),
	}

	config := ssh.ServerConfig{
		NoClientAuth: false,
		// Auth-related things should be constant-time to avoid timing attacks.
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fingerprint := Fingerprint(key)
			if server.IsBanned(fingerprint) {
				return nil, fmt.Errorf("Banned.")
			}
			if !server.IsWhitelisted(fingerprint) {
				return nil, fmt.Errorf("Not Whitelisted.")
			}
			perm := &ssh.Permissions{Extensions: map[string]string{"fingerprint": fingerprint}}
			return perm, nil
		},
		KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			if server.IsBanned("") {
				return nil, fmt.Errorf("Interactive login disabled.")
			}
			if !server.IsWhitelisted("") {
				return nil, fmt.Errorf("Not Whitelisted.")
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)

	server.sshConfig = &config

	return &server, nil
}

// Len returns the number of clients
func (s *Server) Len() int {
	return len(s.clients)
}

// SysMsg broadcasts the given message to everyone
func (s *Server) SysMsg(msg string, args ...interface{}) {
	s.Broadcast(ContinuousFormat(systemMessageFormat, " * "+fmt.Sprintf(msg, args...)), nil)
}

// Broadcast broadcasts the given message to everyone except for the given client
func (s *Server) Broadcast(msg string, except *Client) {
	logger.Debugf("Broadcast to %d: %s", s.Len(), msg)
	s.history.Add(msg)

	s.RLock()
	defer s.RUnlock()

	for _, client := range s.clients {
		if except != nil && client == except {
			continue
		}

		if strings.Contains(msg, client.Name) {
			// Turn message red if client's name is mentioned, and send BEL if they have enabled beeping
			personalMsg := strings.Replace(msg, client.Name, highlightFormat+client.Name+Reset, -1)
			if client.beepMe {
				personalMsg += beep
			}
			client.Send(personalMsg)
		} else {
			if client.quietMode && strings.HasPrefix(msg, systemMessageFormat) {
				continue
			}

			client.Send(msg)
		}
	}
}

// Privmsg sends a message to a particular nick, if it exists
func (s *Server) Privmsg(nick, message string, sender *Client) error {
	// Get the recipient
	target, ok := s.clients[strings.ToLower(nick)]
	if !ok {
		return fmt.Errorf("no client with that nick")
	}
	// Send the message
	target.Msg <- fmt.Sprintf(beep+"[PM from %v] %s%v%s", sender.ColoredName(), privateMessageFormat, message, Reset)
	logger.Debugf("PM from %v to %v: %v", sender.Name, nick, message)
	return nil
}

// SetMotd sets the Message of the Day (MOTD)
func (s *Server) SetMotd(motd string) {
	s.motd = motd
}

// MotdUnicast sends the MOTD as a SysMsg
func (s *Server) MotdUnicast(client *Client) {
	if s.motd == "" {
		return
	}
	client.SysMsg(s.motd)
}

// MotdBroadcast broadcasts the MOTD
func (s *Server) MotdBroadcast(client *Client) {
	if s.motd == "" {
		return
	}
	s.Broadcast(ContinuousFormat(systemMessageFormat, fmt.Sprintf(" * New MOTD set by %s.", client.ColoredName())), client)
	s.Broadcast(s.motd, client)
}

// Add adds the client to the list of clients
func (s *Server) Add(client *Client) {
	go func() {
		s.MotdUnicast(client)
		client.SendLines(s.history.Get(10))
	}()

	s.Lock()
	s.count++

	newName, err := s.proposeName(client.Name)
	if err != nil {
		client.SysMsg("Your name '%s' is not available, renamed to '%s'. Use /nick <name> to change it.", client.Name, ColorString(client.Color, newName))
	}

	client.Rename(newName)
	s.clients[strings.ToLower(client.Name)] = client
	num := len(s.clients)
	s.Unlock()

	s.Broadcast(ContinuousFormat(systemMessageFormat, fmt.Sprintf(" * %s joined. (Total connected: %d)", client.Name, num)), client)
}

// Remove removes the given client from the list of clients
func (s *Server) Remove(client *Client) {
	s.Lock()
	delete(s.clients, strings.ToLower(client.Name))
	s.Unlock()

	s.SysMsg("%s left.", client.Name)
}

func (s *Server) proposeName(name string) (string, error) {
	// Assumes caller holds lock.
	var err error
	name = reStripText.ReplaceAllString(name, "")

	if len(name) > maxNameLength {
		name = name[:maxNameLength]
	} else if len(name) == 0 {
		name = fmt.Sprintf("Guest%d", s.count)
	}

	_, collision := s.clients[strings.ToLower(name)]
	if collision {
		err = fmt.Errorf("%s is not available", name)
		name = fmt.Sprintf("Guest%d", s.count)
	}

	return name, err
}

// Rename renames the given client (user)
func (s *Server) Rename(client *Client, newName string) {
	var oldName string
	if strings.ToLower(newName) == strings.ToLower(client.Name) {
		oldName = client.Name
		client.Rename(newName)
	} else {
		s.Lock()
		newName, err := s.proposeName(newName)
		if err != nil {
			client.SysMsg("%s", err)
			s.Unlock()
			return
		}

		// TODO: Use a channel/goroutine for adding clients, rather than locks?
		delete(s.clients, strings.ToLower(client.Name))
		oldName = client.Name
		client.Rename(newName)
		s.clients[strings.ToLower(client.Name)] = client
		s.Unlock()
	}
	s.SysMsg("%s is now known as %s.", ColorString(client.Color, oldName), ColorString(client.Color, newName))
}

// List lists the clients with the given prefix
func (s *Server) List(prefix *string) []string {
	r := []string{}

	s.RLock()
	defer s.RUnlock()

	for name, client := range s.clients {
		if prefix != nil && !strings.HasPrefix(name, strings.ToLower(*prefix)) {
			continue
		}
		r = append(r, client.Name)
	}

	return r
}

// Who returns the client with a given name
func (s *Server) Who(name string) *Client {
	return s.clients[strings.ToLower(name)]
}

// Op adds the given fingerprint to the list of admins
func (s *Server) Op(fingerprint string) {
	logger.Infof("Adding admin: %s", fingerprint)
	s.Lock()
	s.admins[fingerprint] = struct{}{}
	s.Unlock()
}

// Whitelist adds the given fingerprint to the whitelist
func (s *Server) Whitelist(fingerprint string) error {
	if fingerprint == "" {
		return fmt.Errorf("Invalid fingerprint.")
	}
	if strings.HasPrefix(fingerprint, "github.com/") {
		return s.whitelistIdentityURL(fingerprint)
	}

	return s.whitelistFingerprint(fingerprint)
}

func (s *Server) whitelistIdentityURL(user string) error {
	logger.Infof("Adding github account %s to whitelist", user)

	user = strings.Replace(user, "github.com/", "", -1)
	keys, err := getGithubPubKeys(user)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("No keys for github user %s", user)
	}
	for _, key := range keys {
		fingerprint := Fingerprint(key)
		s.whitelistFingerprint(fingerprint)
	}
	return nil
}

func (s *Server) whitelistFingerprint(fingerprint string) error {
	logger.Infof("Adding whitelist: %s", fingerprint)
	s.Lock()
	s.whitelist[fingerprint] = struct{}{}
	s.Unlock()
	return nil
}

// Client for getting github pub keys
var client = http.Client{
	Timeout: time.Duration(10 * time.Second),
}

// Returns an array of public keys for the given github user URL
func getGithubPubKeys(user string) ([]ssh.PublicKey, error) {
	resp, err := client.Get("http://github.com/" + user + ".keys")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	pubs := []ssh.PublicKey{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "Not Found" {
			continue
		}

		splitKey := strings.SplitN(text, " ", -1)

		// In case of malformated key
		if len(splitKey) < 2 {
			continue
		}

		bodyDecoded, err := base64.StdEncoding.DecodeString(splitKey[1])
		if err != nil {
			return nil, err
		}

		pub, err := ssh.ParsePublicKey(bodyDecoded)
		if err != nil {
			return nil, err
		}

		pubs = append(pubs, pub)
	}
	return pubs, nil
}

// Uptime returns the time since the server was started
func (s *Server) Uptime() string {
	return time.Now().Sub(s.started).String()
}

// IsOp checks if the given client is Op
func (s *Server) IsOp(client *Client) bool {
	_, r := s.admins[client.Fingerprint()]
	return r
}

// IsWhitelisted checks if the given fingerprint is whitelisted
func (s *Server) IsWhitelisted(fingerprint string) bool {
	/* if no whitelist, anyone is welcome */
	if len(s.whitelist) == 0 {
		return true
	}

	/* otherwise, check for whitelist presence */
	_, r := s.whitelist[fingerprint]
	return r
}

// IsBanned checks if the given fingerprint is banned
func (s *Server) IsBanned(fingerprint string) bool {
	ban, hasBan := s.bannedPK[fingerprint]
	if !hasBan {
		return false
	}
	if ban == nil {
		return true
	}
	if ban.Before(time.Now()) {
		s.Unban(fingerprint)
		return false
	}
	return true
}

// Ban bans a fingerprint for the given duration
func (s *Server) Ban(fingerprint string, duration *time.Duration) {
	var until *time.Time
	s.Lock()
	if duration != nil {
		when := time.Now().Add(*duration)
		until = &when
	}
	s.bannedPK[fingerprint] = until
	s.Unlock()
}

// Unban unbans a banned fingerprint
func (s *Server) Unban(fingerprint string) {
	s.Lock()
	delete(s.bannedPK, fingerprint)
	s.Unlock()
}

// Start starts the server
func (s *Server) Start(laddr string) error {
	// Once a ServerConfig has been configured, connections can be
	// accepted.
	socket, err := net.Listen("tcp", laddr)
	if err != nil {
		return err
	}

	logger.Infof("Listening on %s", laddr)

	go func() {
		defer socket.Close()
		for {
			conn, err := socket.Accept()

			if err != nil {
				logger.Errorf("Failed to accept connection: %v", err)
				if err == syscall.EINVAL {
					// TODO: Handle shutdown more gracefully?
					return
				}
			}

			// Goroutineify to resume accepting sockets early.
			go func() {
				// From a standard TCP connection to an encrypted SSH connection
				sshConn, channels, requests, err := ssh.NewServerConn(conn, s.sshConfig)
				if err != nil {
					logger.Errorf("Failed to handshake: %v", err)
					return
				}

				version := reStripText.ReplaceAllString(string(sshConn.ClientVersion()), "")
				if len(version) > 100 {
					version = "Evil Jerk with a superlong string"
				}
				logger.Infof("Connection #%d from: %s, %s, %s", s.count+1, sshConn.RemoteAddr(), sshConn.User(), version)

				go ssh.DiscardRequests(requests)

				client := NewClient(s, sshConn)
				go client.handleChannels(channels)
			}()
		}
	}()

	go func() {
		<-s.done
		socket.Close()
	}()

	return nil
}

// AutoCompleteFunction handles auto completion of nicks
func (s *Server) AutoCompleteFunction(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
	if key == 9 {
		shortLine := strings.Split(line[:pos], " ")
		partialNick := shortLine[len(shortLine)-1]

		nicks := s.List(&partialNick)
		if len(nicks) > 0 {
			nick := nicks[len(nicks)-1]
			posPartialNick := pos - len(partialNick)
			if len(shortLine) < 2 {
				nick += ": "
			} else {
				nick += " "
			}
			newLine = strings.Replace(line[posPartialNick:],
				partialNick, nick, 1)
			newLine = line[:posPartialNick] + newLine
			newPos = pos + (len(nick) - len(partialNick))
			ok = true
		}
	} else {
		ok = false
	}
	return
}

// Stop stops the server
func (s *Server) Stop() {
	s.Lock()
	for _, client := range s.clients {
		client.Conn.Close()
	}
	s.Unlock()

	close(s.done)
}

// Fingerprint returns the fingerprint based on a public key
func Fingerprint(k ssh.PublicKey) string {
	hash := md5.Sum(k.Marshal())
	r := fmt.Sprintf("% x", hash)
	return strings.Replace(r, " ", ":", -1)
}
