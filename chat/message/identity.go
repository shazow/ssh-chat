package message

// Identifier is an interface that can uniquely identify itself.
type Identifier interface {
	Id() string
	SetId(string)
	Name() string
}

// SimpleId is a simple Identifier implementation used for testing.
type SimpleId string

// Id returns the Id as a string.
func (i SimpleId) Id() string {
	return string(i)
}

// SetId is a no-op
func (i SimpleId) SetId(s string) {
	// no-op
}

// Name returns the Id
func (i SimpleId) Name() string {
	return i.Id()
}
