package chat

import "fmt"

const (
	// Reset resets the color
	Reset = "\033[0m"

	// Bold makes the following text bold
	Bold = "\033[1m"

	// Dim dims the following text
	Dim = "\033[2m"

	// Italic makes the following text italic
	Italic = "\033[3m"

	// Underline underlines the following text
	Underline = "\033[4m"

	// Blink blinks the following text
	Blink = "\033[5m"

	// Invert inverts the following text
	Invert = "\033[7m"
)

// Interface for Colors
type Color interface {
	String() string
	Format(string) string
}

// 256 color type, for terminals who support it
type Color256 int8

// String version of this color
func (c Color256) String() string {
	return fmt.Sprintf("38;05;%d", c)
}

// Return formatted string with this color
func (c Color256) Format(s string) string {
	return "\033[" + c.String() + "m" + s + Reset
}

// No color, used for mono theme
type Color0 struct{}

// No-op for Color0
func (c Color0) String() string {
	return ""
}

// No-op for Color0
func (c Color0) Format(s string) string {
	return s
}

// Container for a collection of colors
type Palette struct {
	colors []Color
	size   int
}

// Get a color by index, overflows are looped around.
func (p Palette) Get(i int) Color {
	return p.colors[i%p.size]
}

// Collection of settings for chat
type Theme struct {
	id    string
	sys   Color
	pm    Color
	names *Palette
}

// Colorize name string given some index
func (t Theme) ColorName(s string, i int) string {
	if t.names == nil {
		return s
	}

	return t.names.Get(i).Format(s)
}

// Colorize the PM string
func (t Theme) ColorPM(s string) string {
	if t.pm == nil {
		return s
	}

	return t.pm.Format(s)
}

// Colorize the Sys message
func (t Theme) ColorSys(s string) string {
	if t.sys == nil {
		return s
	}

	return t.sys.Format(s)
}

// List of initialzied themes
var Themes []Theme

// Default theme to use
var DefaultTheme *Theme

func readableColors256() *Palette {
	p := Palette{
		colors: make([]Color, 246),
		size:   246,
	}
	for i := 0; i < 256; i++ {
		if (16 <= i && i <= 18) || (232 <= i && i <= 237) {
			// Remove the ones near black, this is kinda sadpanda.
			continue
		}
		p.colors = append(p.colors, Color256(i))
	}
	return &p
}

func init() {
	palette := readableColors256()

	Themes = []Theme{
		Theme{
			id:    "colors",
			names: palette,
		},
		Theme{
			id: "mono",
		},
	}

	DefaultTheme = &Themes[0]
}
