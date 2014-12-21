package chat

const historyLen = 20

// Channel definition, also a Set of User Items
type Channel struct {
	id      string
	topic   string
	history *History
	users   *Set
	out     chan<- Message
}

func NewChannel(id string, out chan<- Message) *Channel {
	ch := Channel{
		id:      id,
		out:     out,
		history: NewHistory(historyLen),
		users:   NewSet(),
	}
	return &ch
}

func (ch *Channel) Send(m Message) {
	ch.out <- m
}

func (ch *Channel) Join(u *User) error {
	err := ch.users.Add(u)
	return err
}

func (ch *Channel) Leave(u *User) error {
	err := ch.users.Remove(u)
	return err
}

func (ch *Channel) Topic() string {
	return ch.topic
}

func (ch *Channel) SetTopic(s string) {
	ch.topic = s
}
