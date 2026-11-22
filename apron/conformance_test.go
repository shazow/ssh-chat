package apron

import (
	"testing"

	"github.com/apron-chat/apron-server-go/wire/wiretest/conformance"
)

// TestConformance runs the protocol's core checks against the gateway.
func TestConformance(t *testing.T) {
	c := newChat(t)
	conformance.Run(t, conformance.Config{
		Server: c.gateway.Server(),
		Skip: map[string]string{
			// The room's log is ssh-chat's history buffer of 20 messages,
			// fewer than these checks post before they read it back.
			"history.shape.page":       "the history buffer has dropped the earlier messages",
			"history.bounds.inclusive": "the history buffer has dropped the earlier messages",
			// ssh-chat's messages are bounded far below the 16 KiB posts
			// this check makes.
			"core.delivery.slow-reader": "ssh-chat refuses messages this large",
		},
	})
}
