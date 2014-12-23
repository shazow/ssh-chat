package chat

import "fmt"

const historyLen = 20
const channelBuffer = 10

// Channel definition, also a Set of User Items
type Channel struct {
	id        string
	topic     string
	history   *History
	users     *Set
	broadcast chan Message
}

// Create new channel and start broadcasting goroutine.
func NewChannel(id string) *Channel {
	broadcast := make(chan Message, channelBuffer)

	ch := Channel{
		id:        id,
		broadcast: broadcast,
		history:   NewHistory(historyLen),
		users:     NewSet(),
	}

	go func() {
		for m := range broadcast {
			ch.users.Each(func(u Item) {
				u.(*User).Send(m)
			})
		}
	}()

	return &ch
}

func (ch *Channel) Close() {
	close(ch.broadcast)
}

func (ch *Channel) Send(m Message) {
	ch.broadcast <- m
}

func (ch *Channel) Join(u *User) error {
	err := ch.users.Add(u)
	if err != nil {
		return err
	}
	s := fmt.Sprintf("%s joined. (Connected: %d)", u.Name(), ch.users.Len())
	ch.Send(*NewMessage(s))
	return nil
}

func (ch *Channel) Leave(u *User) error {
	err := ch.users.Remove(u)
	if err != nil {
		return err
	}
	s := fmt.Sprintf("%s left.", u.Name())
	ch.Send(*NewMessage(s))
	return nil
}

func (ch *Channel) Topic() string {
	return ch.topic
}

func (ch *Channel) SetTopic(s string) {
	ch.topic = s
}
