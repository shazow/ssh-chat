package sshd

import (
	"context"
	"net"
	"time"

	"github.com/sethvargo/go-limiter"
	"github.com/sethvargo/go-limiter/memorystore"
	"github.com/shazow/rateio"
	"golang.org/x/crypto/ssh"
)

// SSHListener is the container for the connection and ssh-related configuration
type SSHListener struct {
	net.Listener
	config *ssh.ServerConfig

	RateLimit   func() rateio.Limiter
	HandlerFunc func(term *Terminal)

	// handshakeLimit is a semaphore to limit concurrent handshakes globally
	handshakeLimit chan struct{}

	// limiter is the per-IP rate limiter
	limiter limiter.Store
}

// ListenSSH makes an SSH listener socket
func ListenSSH(laddr string, config *ssh.ServerConfig) (*SSHListener, error) {
	socket, err := net.Listen("tcp", laddr)
	if err != nil {
		return nil, err
	}

	// Create a rate limiter: 3 attempts per second per IP?
	// The user wanted to "throttle many connections".
	// 3 per second is generous for a chat server.
	// If an IP connects >3 times in a second, it's likely a bot or flood.
	store, err := memorystore.New(&memorystore.Config{
		Tokens:   3,
		Interval: time.Second,
	})
	if err != nil {
		return nil, err
	}

	l := SSHListener{
		Listener:       socket,
		config:         config,
		handshakeLimit: make(chan struct{}, 20),
		limiter:        store,
	}
	return &l, nil
}

func (l *SSHListener) handleConn(conn net.Conn) (*Terminal, error) {
	if l.RateLimit != nil {
		// TODO: Configurable Limiter?
		conn = ReadLimitConn(conn, l.RateLimit())
	}

	// If the connection doesn't write anything back for too long before we get
	// a valid session, it should be dropped.
	var handleTimeout = 10 * time.Second
	conn.SetReadDeadline(time.Now().Add(handleTimeout))
	defer conn.SetReadDeadline(time.Time{})

	// Upgrade TCP connection to SSH connection
	sshConn, channels, requests, err := ssh.NewServerConn(conn, l.config)
	if err != nil {
		return nil, err
	}

	// FIXME: Disconnect if too many faulty requests? (Avoid DoS.)
	go ssh.DiscardRequests(requests)
	return NewSession(sshConn, channels)
}

// Serve Accepts incoming connections as terminal requests and yield them
func (l *SSHListener) Serve() {
	defer l.Close()
	for {
		conn, err := l.Accept()

		if err != nil {
			logger.Printf("Failed to accept connection: %s", err)
			break
		}

		// Check per-IP limit using go-limiter
		host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
		if err != nil {
			logger.Printf("Failed to split remote addr: %v", err)
		}

		if err == nil {
			// Context with timeout is not strictly needed for memory store, but good practice
			// Although Take is non-blocking for memory store usually.
			_, _, _, ok, err := l.limiter.Take(context.Background(), host)
			if err != nil {
				// Store error (shouldn't happen with memory store unless closed)
				logger.Printf("Rate limiter error: %v", err)
			} else if !ok {
				logger.Printf("[%s] Rejected connection: rate limit exceeded", conn.RemoteAddr())
				conn.Close()
				continue
			}
		}

		// Acquire global semaphore
		l.handshakeLimit <- struct{}{}

		// Goroutineify to resume accepting sockets early
		go func() {
			term, err := l.handleConn(conn)

			// Handshake is done (success or failure). Release limits.
			// Explicit release is required because l.HandlerFunc below
			// runs for the duration of the session. We only want to limit
			// concurrent handshakes, not concurrent sessions.
			<-l.handshakeLimit

			if err != nil {
				logger.Printf("[%s] Failed to handshake: %s", conn.RemoteAddr(), err)
				conn.Close() // Must be closed to avoid a leak
				return
			}
			l.HandlerFunc(term)
		}()
	}
}
