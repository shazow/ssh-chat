package chat

const messageBuffer = 20

// Host knows about all the commands and channels.
type Host struct {
	defaultChannel *Channel
	commands       Commands
	done           chan struct{}
}

func NewHost() *Host {
	h := Host{
		commands: defaultCmdHandlers,
	}
	h.defaultChannel = h.CreateChannel("")
	return &h
}

func (h *Host) handleCommand(m Message) {
	// TODO: ...
}

func (h *Host) broadcast(ch *Channel, m Message) {
	// TODO: ...
}

func (h *Host) CreateChannel(id string) *Channel {
	out := make(chan Message, 20)
	ch := NewChannel(id, out)

	go func() {
		for msg := range out {
			if msg.IsCommand() {
				go h.handleCommand(msg)
				continue
			}
			h.broadcast(ch, msg)
		}
	}()

	return ch
}
