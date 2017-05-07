package message

import "regexp"

// Container for per-user configurations.
type UserConfig struct {
	Highlight *regexp.Regexp
	Bell      bool
	Quiet     bool
	Theme     *Theme
	Seed      int
}

// Default user configuration to use
var DefaultUserConfig UserConfig

func init() {
	DefaultUserConfig = UserConfig{
		Bell:  true,
		Quiet: false,
	}

	// TODO: Seed random?
}
