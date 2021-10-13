package chat

import (
	"fmt"
	"testing"

	"github.com/shazow/ssh-chat/chat/message"
)

func TestAwayCommands(t *testing.T) {
	cmds := &Commands{}
	InitCommands(cmds)

	room := NewRoom()
	go room.Serve()
	defer room.Close()

	// steps are order dependent
	// User can be "away" or "not away" using 3 commands "/away [msg]", "/away", "/back"
	// 2^3 possible cases, run all and verify state at the end
	type step struct {
		// input
		Msg string

		// expected output
		IsUserAway  bool
		AwayMessage string

		// expected state change
		ExpectsError func(awayBefore bool) bool
	}
	neverError := func(_ bool) bool { return false }
	// if the user was away before, then the error is expected
	errorIfAwayBefore := func(awayBefore bool) bool { return awayBefore }

	awayStep := step{"/away snorkling", true, "snorkling", neverError}
	notAwayStep := step{"/away", false, "", errorIfAwayBefore}
	backStep := step{"/back", false, "", errorIfAwayBefore}

	steps := []step{awayStep, notAwayStep, backStep}
	cases := [][]int{
		{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("Case: %d, %d, %d", c[0], c[1], c[2]), func(t *testing.T) {

			u := message.NewUser(message.SimpleID("shark"))

			for _, s := range []step{steps[c[0]], steps[c[1]], steps[c[2]]} {
				msg, _ := message.NewPublicMsg(s.Msg, u).ParseCommand()

				awayBeforeCommand, _, _ := u.GetAway()

				err := cmds.Run(room, *msg)
				if err != nil && s.ExpectsError(awayBeforeCommand) {
					t.Fatalf("unexpected error running the command: %+v", err)
				}

				isAway, _, awayMsg := u.GetAway()
				if isAway != s.IsUserAway {
					t.Fatalf("expected user away state '%t' not equals to actual '%t' after message '%s'", s.IsUserAway, isAway, s.Msg)
				}
				if awayMsg != s.AwayMessage {
					t.Fatalf("expected user away message '%s' not equal to actual '%s' after message '%s'", s.AwayMessage, awayMsg, s.Msg)
				}
			}

		})
	}
}
