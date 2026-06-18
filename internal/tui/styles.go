package tui

import "github.com/charmbracelet/lipgloss"

// Colours
var (
	indigo      = lipgloss.Color("#7C3AED")
	indigoDim   = lipgloss.Color("#5B21B6")
	green       = lipgloss.Color("#22C55E")
	greenDim    = lipgloss.Color("#166534")
	amber       = lipgloss.Color("#F59E0B")
	gray        = lipgloss.Color("#6B7280")
	grayLight   = lipgloss.Color("#9CA3AF")
	grayDark    = lipgloss.Color("#374151")
	grayBg      = lipgloss.Color("#1F2937")
	white       = lipgloss.Color("#F9FAFB")
	red         = lipgloss.Color("#EF4444")
)

// Styles
var (
	// App border — wraps everything
	AppStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(indigo).
			Padding(0, 1)

	// Header
	HeaderStyle = lipgloss.NewStyle().
			Foreground(white).
			Background(indigo).
			Bold(true).
			Padding(0, 2).
			Width(80)

	// Section titles
	SectionStyle = lipgloss.NewStyle().
			Foreground(indigo).
			Bold(true).
			MarginTop(1).
			MarginBottom(1)

	// Stats
	StatLabel = lipgloss.NewStyle().
			Foreground(grayLight).
			Width(12).
			Align(lipgloss.Right)

	StatValue = lipgloss.NewStyle().
			Foreground(white).
			Bold(true)

	StatSeparator = lipgloss.NewStyle().
			Foreground(grayDark).
			SetString(" │ ")

	// Speed
	SpeedStyle = lipgloss.NewStyle().
			Foreground(green).
			Bold(true)

	// Piece map colours
	PieceDone = lipgloss.NewStyle().
			Foreground(green).
			SetString("█")

	PiecePending = lipgloss.NewStyle().
			Foreground(grayDark).
			SetString("░")

	PieceInProgress = lipgloss.NewStyle().
			Foreground(amber).
			SetString("▓")

	// Peer table header
	PeerHeader = lipgloss.NewStyle().
			Foreground(indigo).
			Bold(true).
			Padding(0, 1).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(grayDark)

	PeerCell = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(white)

	PeerChoked = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(gray)

	PeerSeeder = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(green)

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
			Foreground(grayLight).
			Padding(0, 1).
			Width(80)

	KeyStyle = lipgloss.NewStyle().
			Foreground(indigo).
			Bold(true)

	DescStyle = lipgloss.NewStyle().
			Foreground(grayLight)

	// Error
	ErrorStyle = lipgloss.NewStyle().
			Foreground(red).
			Bold(true)

	// Help text
	HelpStyle = lipgloss.NewStyle().
			Foreground(gray).
			Italic(true)
)