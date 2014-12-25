package main

import (
	"fmt"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/sshd"
)

func HandleTerminal(term *sshd.Terminal, channel *chat.Channel) {
	defer term.Close()
	name := term.Conn.User()
	term.SetPrompt(fmt.Sprintf("[%s] ", name))
	// TODO: term.AutoCompleteCallback = ...

	user := chat.NewUserScreen(name, term)
	defer user.Close()

	err := channel.Join(user)
	if err != nil {
		logger.Errorf("Failed to join: %s", err)
		return
	}
	defer func() {
		err := channel.Leave(user)
		if err != nil {
			logger.Errorf("Failed to leave: %s", err)
		}
	}()

	for {
		// TODO: Handle commands etc?
		line, err := term.ReadLine()
		if err != nil {
			// TODO: Catch EOF specifically?
			logger.Errorf("Terminal reading error: %s", err)
			return
		}
		m := chat.NewMessage(line).From(user)
		channel.Send(*m)
	}
}

// Serve a chat service onto the sshd server.
func Serve(listener *sshd.SSHListener) {
	terminals := listener.ServeTerminal()
	channel := chat.NewChannel()

	for term := range terminals {
		go HandleTerminal(term, channel)
	}

}
