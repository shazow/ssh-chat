package sshd

import (
	"fmt"
	"io"
	"sync"
)

type termLine struct {
	Term *Terminal
	Line string
	Err  error
}

func MultiTerm(terms ...*Terminal) *multiTerm {
	mt := &multiTerm{
		lines: make(chan termLine),
	}
	for _, t := range terms {
		mt.Add(t)
	}
	return mt
}

type multiTerm struct {
	AutoCompleteCallback func(line string, pos int, key rune) (newLine string, newPos int, ok bool)

	mu     sync.Mutex
	terms  []*Terminal
	add    chan *Terminal
	lines  chan termLine
	prompt string
}

func (mt *multiTerm) SetPrompt(prompt string) {
	mt.mu.Lock()
	mt.prompt = prompt
	mt.mu.Unlock()
	for _, t := range mt.Terminals() {
		t.SetPrompt(prompt)
	}
}

func (mt *multiTerm) Connections() []Connection {
	terms := mt.Terminals()
	conns := make([]Connection, len(terms))
	for _, term := range terms {
		conns = append(conns, term.Conn)
	}
	return conns
}

func (mt *multiTerm) Terminals() []*Terminal {
	mt.mu.Lock()
	terms := mt.terms
	mt.mu.Unlock()
	return terms
}

func (mt *multiTerm) Add(t *Terminal) {
	mt.mu.Lock()
	mt.terms = append(mt.terms, t)
	prompt := mt.prompt
	mt.mu.Unlock()
	t.AutoCompleteCallback = mt.AutoCompleteCallback
	t.SetPrompt(prompt)

	go func() {
		var line termLine
		for {
			line.Line, line.Err = t.ReadLine()
			line.Term = t
			mt.lines <- line
			if line.Err != nil {
				// FIXME: Should we not abort on all errors?
				break
			}
		}
	}()
}

func (mt *multiTerm) ReadLine() (string, error) {
	line := <-mt.lines
	mt.mu.Lock()
	prompt := mt.prompt
	mt.mu.Unlock()
	if line.Err == nil {
		// Write the line to all the other terminals
		for _, w := range mt.Terminals() {
			if w == line.Term {
				continue
			}
			// XXX: This is super hacky and frankly wrong.
			w.Write([]byte(prompt + line.Line + "\n\r"))
			// TODO: Remove terminal if it fails to write?
		}
	}
	return line.Line, line.Err
}

func (mt *multiTerm) Write(p []byte) (n int, err error) {
	for _, w := range mt.Terminals() {
		n, err = w.Write(p)
		if err != nil {
			return
		}
		if n != len(p) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(p), nil
}

func (mt *multiTerm) Close() error {
	mt.mu.Lock()
	var errs []error
	for _, t := range mt.terms {
		if err := t.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	mt.terms = nil
	mt.mu.Unlock()

	if len(errs) == 0 {
		return nil
	}

	return fmt.Errorf("%d errors: %q", len(errs), errs)
}
