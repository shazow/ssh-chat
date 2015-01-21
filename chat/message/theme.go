package message

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

	// Newline
	Newline = "\r\n"

	// BEL
	Bel = "\007"
)

// Interface for Styles
type Style interface {
	String() string
	Format(string) string
}

// General hardcoded style, mostly used as a crutch until we flesh out the
// framework to support backgrounds etc.
type style string

func (c style) String() string {
	return string(c)
}

func (c style) Format(s string) string {
	return c.String() + s + Reset
}

// 256 color type, for terminals who support it
type Color256 uint8

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
	colors []Style
	size   int
}

// Get a color by index, overflows are looped around.
func (p Palette) Get(i int) Style {
	return p.colors[i%(p.size-1)]
}

func (p Palette) Len() int {
	return p.size
}

func (p Palette) String() string {
	r := ""
	for _, c := range p.colors {
		r += c.Format("X")
	}
	return r
}

// Collection of settings for chat
type Theme struct {
	id        string
	sys       Style
	pm        Style
	highlight Style
	names     *Palette
}

func (t Theme) Id() string {
	return t.id
}

// Colorize name string given some index
func (t Theme) ColorName(u *User) string {
	if t.names == nil {
		return u.Name()
	}

	return t.names.Get(u.colorIdx).Format(u.Name())
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

// Highlight a matched string, usually name
func (t Theme) Highlight(s string) string {
	if t.highlight == nil {
		return s
	}
	return t.highlight.Format(s)
}

// List of initialzied themes
var Themes []Theme

// Default theme to use
var DefaultTheme *Theme

func readableColors256() *Palette {
	size := 247
	p := Palette{
		colors: make([]Style, size),
		size:   size,
	}
	j := 0
	for i := 0; i < 256; i++ {
		if (16 <= i && i <= 18) || (232 <= i && i <= 237) {
			// Remove the ones near black, this is kinda sadpanda.
			continue
		}
		p.colors[j] = Color256(i)
		j++
	}
	return &p
}

func init() {
	palette := readableColors256()

	Themes = []Theme{
		Theme{
			id:        "colors",
			names:     palette,
			sys:       palette.Get(8),                             // Grey
			pm:        palette.Get(7),                             // White
			highlight: style(Bold + "\033[48;5;11m\033[38;5;16m"), // Yellow highlight
		},
		Theme{
			id: "mono",
		},
	}

	// Debug for printing colors:
	//for _, color := range palette.colors {
	//	fmt.Print(color.Format(color.String() + " "))
	//}

	DefaultTheme = &Themes[0]
}
