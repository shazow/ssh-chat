package chat

import "fmt"

const historyLen = 20

// Channel definition, also a Set of User Items
type Channel struct {
	id        string
	topic     string
	history   *History
	users     *Set
	broadcast chan<- Message
}

func NewChannel(id string, broadcast chan<- Message) *Channel {
	ch := Channel{
		id:        id,
		broadcast: broadcast,
		history:   NewHistory(historyLen),
		users:     NewSet(),
	}
	return &ch
}

func (ch *Channel) Send(m Message) {
	ch.broadcast <- m
}

func (ch *Channel) Join(u *User) error {
	err := ch.users.Add(u)
	if err != nil {
		s := fmt.Sprintf("%s joined. (Connected: %d)", u.Name(), ch.users.Len())
		ch.Send(*NewMessage(s))
	}
	return err
}

func (ch *Channel) Leave(u *User) error {
	err := ch.users.Remove(u)
	if err != nil {
		s := fmt.Sprintf("%s left.", u.Name())
		ch.Send(*NewMessage(s))
	}
	return err
}

func (ch *Channel) Topic() string {
	return ch.topic
}

func (ch *Channel) SetTopic(s string) {
	ch.topic = s
}
