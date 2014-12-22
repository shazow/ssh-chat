package sshd

import (
	"fmt"
	"io"
	"strings"
)

// Keep track of multiple errors and coerce them into one error
type MultiError []error

func (e MultiError) Error() string {
	switch len(e) {
	case 0:
		return ""
	case 1:
		return e[0].Error()
	default:
		errs := []string{}
		for _, err := range e {
			errs = append(errs, err.Error())
		}
		return fmt.Sprintf("%d errors: %s", strings.Join(errs, "; "))
	}
}

// Keep track of multiple closers and close them all as one closer
type MultiCloser []io.Closer

func (c MultiCloser) Close() error {
	errors := MultiError{}
	for _, closer := range c {
		err := closer.Close()
		if err != nil {
			errors = append(errors, err)
		}
	}
	if len(errors) == 0 {
		return nil
	}
	return errors
}
