// Package styles holds the theme catalog and shared style tokens.
package styles

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Theme is one selectable palette. Unlike the editor TUI there is no syntax
// highlighter here, so the catalog carries only the interface colours.
type Theme struct {
	Name        string
	Description string
	Accent      string
	Muted       string
	Border      string
	Green       string
	Red         string
	Yellow      string
	Blue        string
	ActiveBg    string
}

// catalog is the ordered list of themes; the first one is the default.
var catalog = []Theme{
	{
		Name:        "tokyonight-night",
		Description: "Tokyo Night",
		Accent:      "#7aa2f7",
		Muted:       "#9aa5ce",
		Border:      "#565f89",
		Green:       "#9ece6a",
		Red:         "#f7768e",
		Yellow:      "#e0af68",
		Blue:        "#7dcfff",
		ActiveBg:    "#1f3b2d",
	},
	{
		Name:        "catppuccin-mocha",
		Description: "Catppuccin Mocha",
		Accent:      "#89b4fa",
		Muted:       "#a6adc8",
		Border:      "#585b70",
		Green:       "#a6e3a1",
		Red:         "#f38ba8",
		Yellow:      "#f9e2af",
		Blue:        "#89dceb",
		ActiveBg:    "#26332a",
	},
	{
		Name:        "dracula",
		Description: "Dracula",
		Accent:      "#bd93f9",
		Muted:       "#6272a4",
		Border:      "#44475a",
		Green:       "#50fa7b",
		Red:         "#ff5555",
		Yellow:      "#f1fa8c",
		Blue:        "#8be9fd",
		ActiveBg:    "#1e3a2a",
	},
	{
		Name:        "gruvbox-dark",
		Description: "Gruvbox Dark",
		Accent:      "#83a598",
		Muted:       "#928374",
		Border:      "#504945",
		Green:       "#b8bb26",
		Red:         "#fb4934",
		Yellow:      "#fabd2f",
		Blue:        "#8ec07c",
		ActiveBg:    "#2b331f",
	},
}

// Themes returns a copy of the catalog in selection order.
func Themes() []Theme {
	out := make([]Theme, len(catalog))
	copy(out, catalog)

	return out
}

// DefaultTheme returns Tokyo Night, the palette the TUI ships with.
func DefaultTheme() Theme {
	return catalog[0]
}

// ThemeByName resolves a theme by its exact name, ignoring surrounding space.
func ThemeByName(name string) (Theme, bool) {
	want := strings.TrimSpace(name)
	for _, theme := range catalog {
		if theme.Name == want {
			return theme, true
		}
	}

	return Theme{}, false
}

// ThemeNames lists the catalog names in order.
func ThemeNames() []string {
	names := make([]string, 0, len(catalog))
	for _, theme := range catalog {
		names = append(names, theme.Name)
	}

	return names
}

// current is the palette the tokens were last built from.
var current Theme

// PlainCells keeps tabs untouched, so the rendered rows match the parsed text.
func PlainCells(style lipgloss.Style) lipgloss.Style {
	return style.TabWidth(lipgloss.NoTabConversion)
}

// Border frames an overlay or an unfocused panel.
var Border = lipgloss.NewStyle()

// BorderFocused frames the panel that owns the keyboard: the accent colour plus the statusbar cue.
var BorderFocused = lipgloss.NewStyle()

// Help is the keymap overlay: the neutral frame plus its inner padding.
var Help = lipgloss.NewStyle()

// Tokens of text that never carries tabs: the layout chrome, the list and the state lines.
var (
	Title        = lipgloss.NewStyle()
	Tab          = lipgloss.NewStyle()
	TabActive    = lipgloss.NewStyle()
	TabSeparator = lipgloss.NewStyle()
	Status       = lipgloss.NewStyle()
	HelpTitle    = lipgloss.NewStyle()
	HelpKey      = lipgloss.NewStyle()
	Placeholder  = lipgloss.NewStyle()
	Spinner      = lipgloss.NewStyle()

	Cursor = lipgloss.NewStyle()
	Fail   = lipgloss.NewStyle()
	Warn   = lipgloss.NewStyle()
)

// The Plain tokens keep the tabs of the rows they render: hosts, tunnels and logs.
var (
	TitlePlain     = lipgloss.NewStyle()
	TabPlain       = lipgloss.NewStyle()
	TabActivePlain = lipgloss.NewStyle()
	CursorPlain    = lipgloss.NewStyle()
	MetaPlain      = lipgloss.NewStyle()
	Header         = lipgloss.NewStyle()
	Gutter         = lipgloss.NewStyle()
	FailPlain      = lipgloss.NewStyle()
	WarnPlain      = lipgloss.NewStyle()
	ActivePlain    = lipgloss.NewStyle()
	TagPlain       = lipgloss.NewStyle()
	ValuePlain     = lipgloss.NewStyle()
)

// Apply rebuilds every token from theme and makes it the active palette.
func Apply(theme Theme) {
	current = theme

	Border = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.Border))
	BorderFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.Accent))
	Help = Border.Padding(0, 2)

	Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.Accent))
	Tab = lipgloss.NewStyle().Faint(true)
	TabActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.Accent))
	TabSeparator = lipgloss.NewStyle().Faint(true)
	Status = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted))
	HelpTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.Accent))
	HelpKey = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Green))
	Placeholder = lipgloss.NewStyle().Faint(true)
	Spinner = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent))

	Cursor = lipgloss.NewStyle().Bold(true).Reverse(true)
	Fail = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Red))
	Warn = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Yellow))

	TitlePlain = PlainCells(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.Accent)))
	TabPlain = PlainCells(lipgloss.NewStyle().Faint(true))
	TabActivePlain = PlainCells(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.Accent)))
	CursorPlain = PlainCells(lipgloss.NewStyle().Bold(true).Reverse(true))
	MetaPlain = PlainCells(lipgloss.NewStyle().Faint(true))
	Header = PlainCells(lipgloss.NewStyle().Faint(true))
	Gutter = PlainCells(lipgloss.NewStyle().Faint(true))
	FailPlain = PlainCells(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Red)))
	WarnPlain = PlainCells(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Yellow)))
	ActivePlain = PlainCells(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Green)))
	TagPlain = PlainCells(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Blue)))
	ValuePlain = PlainCells(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted)))
}

// CurrentTheme returns the palette the tokens were last built from.
func CurrentTheme() Theme {
	return current
}

// init paints the tokens with the default palette, so the package renders on load.
func init() {
	Apply(DefaultTheme())
}
