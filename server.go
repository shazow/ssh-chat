package main

import (
	"crypto/md5"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

const MAX_NAME_LENGTH = 32
const HISTORY_LEN = 20

const SYSTEM_MESSAGE_FORMAT string = "\033[1;3;90m"
const PRIVATE_MESSAGE_FORMAT string = "\033[3m"
const BEEP string = "\007"

var RE_STRIP_TEXT = regexp.MustCompile("[^0-9A-Za-z_.-]")

type Clients map[string]*Client

type Server struct {
	sshConfig *ssh.ServerConfig
	done      chan struct{}
	clients   Clients
	lock      sync.Mutex
	count     int
	history   *History
	motd      string
	admins    map[string]struct{}   // fingerprint lookup
	bannedPk  map[string]*time.Time // fingerprint lookup
	bannedIp  map[net.Addr]*time.Time
	started   time.Time
}

func NewServer(privateKey []byte) (*Server, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	server := Server{
		done:     make(chan struct{}),
		clients:  Clients{},
		count:    0,
		history:  NewHistory(HISTORY_LEN),
		motd:     "Message of the Day! Modify with /motd",
		admins:   map[string]struct{}{},
		bannedPk: map[string]*time.Time{},
		bannedIp: map[net.Addr]*time.Time{},
		started:  time.Now(),
	}

	config := ssh.ServerConfig{
		NoClientAuth: false,
		// Auth-related things should be constant-time to avoid timing attacks.
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fingerprint := Fingerprint(key)
			if server.IsBanned(fingerprint) {
				return nil, fmt.Errorf("Banned.")
			}
			perm := &ssh.Permissions{Extensions: map[string]string{"fingerprint": fingerprint}}
			return perm, nil
		},
	}
	config.AddHostKey(signer)

	server.sshConfig = &config

	return &server, nil
}

func (s *Server) Len() int {
	return len(s.clients)
}

func (s *Server) SysMsg(msg string, args ...interface{}) {
	s.Broadcast(ContinuousFormat(SYSTEM_MESSAGE_FORMAT, " * "+fmt.Sprintf(msg, args...)), nil)
}

func (s *Server) Broadcast(msg string, except *Client) {
	logger.Debugf("Broadcast to %d: %s", s.Len(), msg)
	s.history.Add(msg)

	for _, client := range s.clients {
		if (except != nil && client == except) || client.Channel != except.Channel {
			continue
		}

		if except != nil && except.Channel != "" {
			msg = "#" + except.Channel + "> " + msg
		}
		if strings.Contains(msg, client.Name) {
			// Turn message red if client's name is mentioned, and send BEL if they have enabled beeping
			tmpMsg := strings.Split(msg, RESET)
			if client.beepMe {
				tmpMsg[0] += BEEP
			}
			client.Send(strings.Join(tmpMsg, RESET+BOLD+"\033[31m") + RESET)
		} else {
			client.Send(msg)
		}
	}
}

/* Send a message to a particular nick, if it exists */
func (s *Server) Privmsg(nick, message string, sender *Client) error {
	/* Get the recipient */
	target, ok := s.clients[nick]
	if !ok {
		return fmt.Errorf("no client with that nick")
	}
	/* Send the message */
	target.Msg <- fmt.Sprintf(BEEP+"[PM from %v] %s%v%s", sender.ColoredName(), PRIVATE_MESSAGE_FORMAT, message, RESET)
	logger.Debugf("PM from %v to %v: %v", sender.Name, nick, message)
	return nil
}

func (s *Server) SetMotd(client *Client, motd string) {
	s.lock.Lock()
	s.motd = motd
	s.lock.Unlock()
}

func (s *Server) MotdUnicast(client *Client) {
	client.SysMsg("/** MOTD")
	client.SysMsg(" * " + ColorString("36", s.motd)) /* a nice cyan color */
	client.SysMsg(" **/")
}

func (s *Server) MotdBroadcast(client *Client) {
	s.Broadcast(ContinuousFormat(SYSTEM_MESSAGE_FORMAT, fmt.Sprintf(" * New MOTD set by %s.", client.ColoredName())), client)
	s.Broadcast(" /**\r\n"+"  * "+ColorString("36", s.motd)+"\r\n  **/", client)
}

func (s *Server) Add(client *Client) {
	go func() {
		s.MotdUnicast(client)
		client.SendLines(s.history.Get(10))
		client.SysMsg("Welcome to ssh-chat. Enter /help for more.")
	}()

	s.lock.Lock()
	s.count++

	newName, err := s.proposeName(client.Name)
	if err != nil {
		client.SysMsg("Your name '%s' is not available, renamed to '%s'. Use /nick <name> to change it.", client.ColoredName(), ColorString(client.Color, newName))
	}

	client.Rename(newName)
	s.clients[client.Name] = client
	num := len(s.clients)
	s.lock.Unlock()

	s.Broadcast(ContinuousFormat(SYSTEM_MESSAGE_FORMAT, fmt.Sprintf(" * %s joined. (Total connected: %d)", client.ColoredName(), num)), client)
}

func (s *Server) Remove(client *Client) {
	s.lock.Lock()
	delete(s.clients, client.Name)
	s.lock.Unlock()

	s.SysMsg("%s left.", client.ColoredName())
}

func (s *Server) proposeName(name string) (string, error) {
	// Assumes caller holds lock.
	var err error
	name = RE_STRIP_TEXT.ReplaceAllString(name, "")

	if len(name) > MAX_NAME_LENGTH {
		name = name[:MAX_NAME_LENGTH]
	} else if len(name) == 0 {
		name = fmt.Sprintf("Guest%d", s.count)
	}

	_, collision := s.clients[name]
	if collision {
		err = fmt.Errorf("%s is not available.", name)
		name = fmt.Sprintf("Guest%d", s.count)
	}

	return name, err
}

func (s *Server) Rename(client *Client, newName string) {
	s.lock.Lock()

	newName, err := s.proposeName(newName)
	if err != nil {
		client.SysMsg("%s", err)
		s.lock.Unlock()
		return
	}

	// TODO: Use a channel/goroutine for adding clients, rathern than locks?
	delete(s.clients, client.Name)
	oldName := client.Name
	client.Rename(newName)
	s.clients[client.Name] = client
	s.lock.Unlock()

	s.SysMsg("%s is now known as %s.", ColorString(client.Color, oldName), ColorString(client.Color, newName))
}

func (s *Server) List(prefix *string) []string {
	r := []string{}

	for name, _ := range s.clients {
		if prefix != nil && !strings.HasPrefix(name, *prefix) {
			continue
		}
		r = append(r, name)
	}

	return r
}

func (s *Server) Who(name string) *Client {
	return s.clients[name]
}

func (s *Server) Op(fingerprint string) {
	logger.Infof("Adding admin: %s", fingerprint)
	s.lock.Lock()
	s.admins[fingerprint] = struct{}{}
	s.lock.Unlock()
}

func (s *Server) Uptime() string {
	return time.Now().Sub(s.started).String()
}

func (s *Server) IsOp(client *Client) bool {
	_, r := s.admins[client.Fingerprint()]
	return r
}

func (s *Server) IsBanned(fingerprint string) bool {
	ban, hasBan := s.bannedPk[fingerprint]
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

func (s *Server) Ban(fingerprint string, duration *time.Duration) {
	var until *time.Time
	s.lock.Lock()
	if duration != nil {
		when := time.Now().Add(*duration)
		until = &when
	}
	s.bannedPk[fingerprint] = until
	s.lock.Unlock()
}

func (s *Server) Unban(fingerprint string) {
	s.lock.Lock()
	delete(s.bannedPk, fingerprint)
	s.lock.Unlock()
}

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

				version := RE_STRIP_TEXT.ReplaceAllString(string(sshConn.ClientVersion()), "")
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

func (s *Server) Stop() {
	for _, client := range s.clients {
		client.Conn.Close()
	}

	close(s.done)
}

func Fingerprint(k ssh.PublicKey) string {
	hash := md5.Sum(k.Marshal())
	r := fmt.Sprintf("% x", hash)
	return strings.Replace(r, " ", ":", -1)
}
