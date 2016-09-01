package message

// Identifier is an interface that can uniquely identify itself.
type Identifier interface {
	ID() string
	Name() string
	SetName(string)
}

// SimpleID is a simple Identifier implementation used for testing.
type SimpleID string

// ID returns the ID as a string.
func (i SimpleID) ID() string {
	return string(i)
}

// Name returns the ID
func (i SimpleID) Name() string {
	return i.ID()
}

// SetName is a no-op
func (i SimpleID) SetName(s string) {
	// no-op
}
