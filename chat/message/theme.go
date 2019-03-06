package message

import (
	"fmt"
	"time"
)

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

func Color256Palette(colors ...uint8) *Palette {
	size := len(colors)
	p := make([]Style, 0, size)
	for _, color := range colors {
		p = append(p, Color256(color))
	}
	return &Palette{
		colors: p,
		size:   size,
	}
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
	if p.size == 1 {
		return p.colors[0]
	}
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

func (theme Theme) ID() string {
	return theme.id
}

// Colorize name string given some index
func (theme Theme) ColorName(u *User) string {
	if theme.names == nil {
		return u.Name()
	}

	return theme.names.Get(u.colorIdx).Format(u.Name())
}

// Colorize the PM string
func (theme Theme) ColorPM(s string) string {
	if theme.pm == nil {
		return s
	}

	return theme.pm.Format(s)
}

// Colorize the Sys message
func (theme Theme) ColorSys(s string) string {
	if theme.sys == nil {
		return s
	}

	return theme.sys.Format(s)
}

// Highlight a matched string, usually name
func (theme Theme) Highlight(s string) string {
	if theme.highlight == nil {
		return s
	}
	return theme.highlight.Format(s)
}

// Timestamp formats and colorizes the timestamp.
func (theme Theme) Timestamp(t time.Time) string {
	// TODO: Change this per-theme? Or config?
	return theme.sys.Format(t.Format("2006-01-02 15:04 UTC"))
}

// List of initialzied themes
var Themes []Theme

// Default theme to use
var DefaultTheme *Theme

func allColors256() *Palette {
	colors := []uint8{}
	var i uint8
	for i = 0; i < 255; i++ {
		colors = append(colors, i)
	}
	return Color256Palette(colors...)
}

func readableColors256() *Palette {
	colors := []uint8{}
	var i uint8
	for i = 0; i < 255; i++ {
		if i == 0 || i == 7 || i == 8 || i == 15 || i == 16 || i == 17 || i > 230 {
			// Skip 31 Shades of Grey, and one hyperintelligent shade of blue.
			continue
		}
		colors = append(colors, i)
	}
	return Color256Palette(colors...)
}

func init() {
	Themes = []Theme{
		{
			id:        "colors",
			names:     readableColors256(),
			sys:       Color256(245),                              // Grey
			pm:        Color256(7),                                // White
			highlight: style(Bold + "\033[48;5;11m\033[38;5;16m"), // Yellow highlight
		},
		{
			id:        "solarized",
			names:     Color256Palette(1, 2, 3, 4, 5, 6, 7, 9, 13),
			sys:       Color256(11),                              // Yellow
			pm:        Color256(15),                              // White
			highlight: style(Bold + "\033[48;5;3m\033[38;5;94m"), // Orange highlight
		},
		{
			id:        "hacker",
			names:     Color256Palette(82),                        // Green
			sys:       Color256(22),                               // Another green
			pm:        Color256(28),                               // More green, slightly lighter
			highlight: style(Bold + "\033[48;5;22m\033[38;5;46m"), // Green on dark green
		},
		{
			id: "mono",
		},
	}

	DefaultTheme = &Themes[0]

	/* Some debug helpers for your convenience:

	// Debug for palettes
	printPalette(allColors256())

	// Debug for themes
	for _, t := range Themes {
		printTheme(t)
	}

	*/
}

func printTheme(t Theme) {
	fmt.Println("Printing theme:", t.ID())
	if t.names != nil {
		for i, color := range t.names.colors {
			fmt.Printf("%s ", color.Format(fmt.Sprintf("name%d", i)))
		}
		fmt.Println("")
	}
	fmt.Println(t.ColorSys("SystemMsg"))
	fmt.Println(t.ColorPM("PrivateMsg"))
	fmt.Println(t.Highlight("Highlight"))
	fmt.Println("")
}

func printPalette(p *Palette) {
	for i, color := range p.colors {
		fmt.Printf("%d\t%s\n", i, color.Format(color.String()+" "))
	}
}
