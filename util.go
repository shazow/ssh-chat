package sshchat

import "fmt"

type multiError []error

func (err multiError) Error() string {
	if len(err) == 0 {
		return ""
	}
	return fmt.Sprintf("%d errors: %q", len(err), err)
}
