package apron

import (
	"testing"

	"github.com/apron-chat/apron-server-go/wire/wiretest/conformance"
)

// TestConformance runs the protocol's core checks against the gateway. The
// checks that need more than ssh-chat keeps (a 20-message history) or
// accepts (16 KiB messages) end with a WARN of the limit, not a failure.
func TestConformance(t *testing.T) {
	c := newChat(t)
	conformance.Run(t, conformance.Config{Server: c.gateway.Server()})
}
