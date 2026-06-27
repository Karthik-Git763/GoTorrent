package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"go-torrent/internal/piece"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

// TickMsg fires periodically to refresh stats.
type TickMsg struct{}

// DownloadDoneMsg signals the download finished.
type DownloadDoneMsg struct{ Err error }

type transferSample struct {
	at    time.Time
	bytes uint64
}

// Model is the Bubble Tea model for the GoTorrent TUI.
type Model struct {
	m        *piece.Manager
	width    int
	height   int
	ready    bool
	percent  float64 // 0.0 – 1.0, set every tick
	progress progress.Model
	done     bool
	err      error
	doneCh   <-chan error // closed when download completes

	// Speed tracking
	transferSamples []transferSample
	downloadRate    float64 // bytes/sec

	// Piece map cache
	pieceMapWidth int
}

// NewModel creates a new TUI model.
func NewModel(m *piece.Manager, doneCh <-chan error) Model {
	return Model{
		m:               m,
		doneCh:          doneCh,
		transferSamples: []transferSample{{at: time.Now(), bytes: m.TransferredBytes()}},
		pieceMapWidth:   60,
		progress:        progress.New(progress.WithSolidFill("#22C55E"), progress.WithoutPercentage()),
	}
}

// Init initializes the program with a tick command for periodic updates
// and a wait-for-done command.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.tickCmd(),
		m.waitForDone(),
	)
}

// tickCmd returns a command that fires a TickMsg after the refresh interval.
// It is re-scheduled from the TickMsg handler so the TUI updates continuously.
func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

// waitForDone returns a Cmd that blocks until the download finishes.
func (m Model) waitForDone() tea.Cmd {
	return func() tea.Msg {
		err, ok := <-m.doneCh
		if !ok {
			return nil
		}
		return DownloadDoneMsg{Err: err}
	}
}

// Update handles messages and returns the updated model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.pieceMapWidth = msg.Width - 10
		if m.pieceMapWidth < 20 {
			m.pieceMapWidth = 20
		}
		m.progress.Width = msg.Width - 14
		if m.progress.Width < 10 {
			m.progress.Width = 10
		}
		return m, nil

	case TickMsg:
		if m.done || m.err != nil {
			return m, nil
		}
		m.updateStats()
		return m, m.tickCmd()

	case DownloadDoneMsg:
		m.done = true
		if msg.Err != nil {
			m.err = msg.Err
		}
		// Don't quit immediately on error — wait for user keypress
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
		return m, nil
	}

	return m, nil
}

// updateStats reads the manager's current state and updates the model.
func (m *Model) updateStats() {
	now := time.Now()
	m.transferSamples = append(m.transferSamples, transferSample{
		at:    now,
		bytes: m.m.TransferredBytes(),
	})
	m.updateDownloadRate(now, 3*time.Second)

	// Progress is based on exact verified bytes, including a short final piece.
	totalLength := m.m.TotalLength()
	if totalLength > 0 {
		m.percent = float64(m.m.CompletedBytes()) / float64(totalLength)
	}
	if m.percent < 0 {
		m.percent = 0
	}
	if m.percent > 1 {
		m.percent = 1
	}
}

func (m *Model) updateDownloadRate(now time.Time, window time.Duration) {
	cutoff := now.Add(-window)
	first := 0
	for first+1 < len(m.transferSamples) && !m.transferSamples[first+1].at.After(cutoff) {
		first++
	}
	m.transferSamples = m.transferSamples[first:]

	oldest := m.transferSamples[0]
	latest := m.transferSamples[len(m.transferSamples)-1]
	elapsed := latest.at.Sub(oldest.at).Seconds()
	if elapsed <= 0 || latest.bytes < oldest.bytes {
		m.downloadRate = 0
		return
	}
	m.downloadRate = float64(latest.bytes-oldest.bytes) / elapsed
}

// View renders the terminal UI.
func (m Model) View() tea.View {
	if !m.ready {
		status := m.m.Status()
		if status == "" {
			status = "Initialising..."
		}
		return tea.NewView(status)
	}

	if m.err != nil {
		return tea.NewView(ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}

	if m.done {
		return tea.NewView(m.renderComplete())
	}

	return tea.NewView(m.renderMain())
}

// renderMain renders the main TUI layout during download.
func (m Model) renderMain() string {
	var out strings.Builder

	// Header
	out.WriteString(m.renderHeader())
	out.WriteString("\n")

	// Progress bar
	out.WriteString(m.renderProgress())
	out.WriteString("\n")

	// Stats row
	out.WriteString(m.renderStats())
	out.WriteString("\n")

	// Piece map
	out.WriteString(m.renderPieceMap())
	out.WriteString("\n\n")

	// Peer table
	out.WriteString(m.renderPeerTable())
	out.WriteString("\n")

	// Status bar
	out.WriteString(m.renderStatusBar())

	return AppStyle.Render(out.String())
}

// renderComplete shows a completion screen.
func (m Model) renderComplete() string {
	var out strings.Builder
	if m.err != nil {
		out.WriteString(ErrorStyle.Render(fmt.Sprintf("Download failed: %v\n", m.err)))
	} else {
		out.WriteString(lipgloss.NewStyle().Foreground(green).Bold(true).Render("Download Complete\n\n"))
	}
	out.WriteString("Press q to exit")
	return AppStyle.Render(out.String())
}

// renderHeader renders the torrent name header.
func (m Model) renderHeader() string {
	name := m.m.TorrentName()
	if len(name) > 60 {
		name = name[:57] + "..."
	}
	title := lipgloss.NewStyle().Foreground(white).Bold(true).Render(name)
	label := lipgloss.NewStyle().Foreground(indigo).Render("GoTorrent")
	status := m.m.Status()
	if status == "" {
		status = "Idle"
	}
	statusLine := lipgloss.NewStyle().Foreground(gray).Render(status)
	return fmt.Sprintf("  %s  %s\n  %s", label, title, statusLine)
}

// renderProgress renders the official bubbles/progress bar with percentage on the right.
func (m Model) renderProgress() string {
	return fmt.Sprintf("  %s  %5.1f%%", m.progress.ViewAs(m.percent), m.percent*100)
}

// renderStats renders the stats row.
func (m Model) renderStats() string {
	completed := piece.CountCompleted(m.m.Completed())
	total := m.m.TotalPieces()
	peers := len(m.m.Peers())

	downloaded := int64(m.m.CompletedBytes())
	speed := m.formatSpeed(m.downloadRate)
	eta := m.formatETA(m.downloadRate, int64(m.m.TotalLength())-downloaded)
	size := m.formatBytes(int64(m.m.TotalLength()))

	sep := lipgloss.NewStyle().Foreground(grayDark).Render(" │ ")

	statLine := StatLabel.Render("Pieces:") +
		StatValue.Render(fmt.Sprintf("%d/%d", completed, total)) +
		sep +
		StatLabel.Render("Peers:") +
		StatValue.Render(fmt.Sprintf("%d", peers)) +
		sep +
		StatLabel.Render("Speed:") +
		SpeedStyle.Render(speed) +
		sep +
		StatLabel.Render("Size:") +
		StatValue.Render(size) +
		sep +
		StatLabel.Render("ETA:") +
		StatValue.Render(eta)

	return "  " + statLine
}

// renderPieceMap renders a visual grid of piece completion status.
func (m Model) renderPieceMap() string {
	var out strings.Builder
	out.WriteString(SectionStyle.Render("Piece Map"))
	out.WriteString("\n")

	completed := m.m.Completed()
	totalPieces := m.m.TotalPieces()

	if totalPieces == 0 {
		return out.String()
	}

	width := m.pieceMapWidth
	if width <= 0 {
		width = 60
	}

	var row strings.Builder
	row.WriteString("  ")
	for i := 0; i < width && i < totalPieces; i++ {
		pieceIdx := int(float64(i) / float64(width) * float64(totalPieces))
		if pieceIdx >= len(completed) {
			row.WriteString(PiecePending.String())
		} else if completed[pieceIdx] {
			row.WriteString(PieceDone.String())
		} else {
			row.WriteString(PiecePending.String())
		}
	}
	out.WriteString(row.String())
	return out.String()
}

// renderPeerTable renders the peers table with real addresses.
func (m Model) renderPeerTable() string {
	var out strings.Builder
	peers := m.m.Peers()

	out.WriteString(SectionStyle.Render(fmt.Sprintf("Peers (%d)", len(peers))))
	out.WriteString("\n")

	if len(peers) == 0 {
		out.WriteString("  ")
		out.WriteString(HelpStyle.Render("No peers connected"))
		return out.String()
	}

	// Column widths
	addrW := 22
	statusW := 12

	// Header row (no decoration, just aligned)
	header := fmt.Sprintf("  %-*s %*s",
		addrW, "Address",
		statusW, "Status")
	out.WriteString(header)
	out.WriteString("\n")

	// Separator line
	sep := fmt.Sprintf("  %s %s ",
		strings.Repeat("─", addrW),
		strings.Repeat("─", statusW))
	out.WriteString(sep)
	out.WriteString("\n")

	// Rows — show up to 10
	maxRows := 10
	for i, p := range peers {
		if i >= maxRows {
			break
		}
		addr := p.RemoteAddr()
		// Shorten IPv6 or long addresses
		if len(addr) > addrW {
			addr = addr[:addrW-3] + "..."
		}
		bf := p.Bitfield()
		count := 0
		for _, v := range bf {
			if v {
				count++
			}
		}

		line := fmt.Sprintf("  %-*s %-*s",
			addrW, addr,
			statusW, "active")
		out.WriteString(line)
		out.WriteString("\n")
	}

	if len(peers) > maxRows {
		out.WriteString("  ")
		out.WriteString(HelpStyle.Render(fmt.Sprintf("... and %d more", len(peers)-maxRows)))
		out.WriteString("\n")
	}

	return out.String()
}

// renderStatusBar renders the bottom status bar with keyboard shortcuts.
func (m Model) renderStatusBar() string {
	keys := []struct{ key, desc string }{
		{"q", "quit"},
	}

	left := ""
	for _, k := range keys {
		left += KeyStyle.Render(k.key) + " " + DescStyle.Render(k.desc) + "  "
	}

	return StatusBarStyle.Render(left)
}

// Helpers

func (m Model) formatSpeed(bytesPerSec float64) string {
	switch {
	case bytesPerSec >= 1_000_000:
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/1_000_000)
	case bytesPerSec >= 1_000:
		return fmt.Sprintf("%.0f KB/s", bytesPerSec/1_000)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

func (m Model) formatBytes(b int64) string {
	switch {
	case b >= 1_000_000_000:
		return fmt.Sprintf("%.1f GB", float64(b)/1_000_000_000)
	case b >= 1_000_000:
		return fmt.Sprintf("%.1f MB", float64(b)/1_000_000)
	case b >= 1_000:
		return fmt.Sprintf("%.0f KB", float64(b)/1_000)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func (m Model) formatETA(speed float64, remaining int64) string {
	if speed <= 0 || remaining <= 0 {
		return "—"
	}
	secs := float64(remaining) / speed
	if math.IsInf(secs, 0) || math.IsNaN(secs) {
		return "—"
	}
	if secs < 60 {
		return fmt.Sprintf("%.0fs", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("%.0fm %.0fs", secs/60, math.Mod(secs, 60))
	}
	return fmt.Sprintf("%.0fh %.0fm", secs/3600, math.Mod(secs/60, 60))
}

// Run starts the Bubble Tea program with the given model.
// doneCh receives nil on success or an error on failure.
func Run(m *piece.Manager, doneCh <-chan error) error {
	model := NewModel(m, doneCh)
	p := tea.NewProgram(model)
	_, err := p.Run()
	return err
}
