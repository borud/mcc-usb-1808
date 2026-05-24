package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const (
	yAxisWidth = 8   // chars reserved for Y-axis labels
	yMax       = 6.0 // fixed display range: ±6 V (amplitude is ±5 V)
	yMin       = -6.0
	ySpan      = yMax - yMin
)

// Viewer maintains a rolling circular buffer of float64 samples and
// renders an ASCII plot in the terminal, refreshing at 10 Hz.
type Viewer struct {
	mu    sync.Mutex
	buf   []float64
	cap   int
	head  int
	count int
}

// NewViewer returns a Viewer that retains the last windowSize samples.
func NewViewer(windowSize int) *Viewer {
	return &Viewer{
		buf: make([]float64, windowSize),
		cap: windowSize,
	}
}

// Push appends val to the rolling buffer. Safe for concurrent use.
func (v *Viewer) Push(val float64) {
	v.mu.Lock()
	v.buf[v.head] = val
	v.head = (v.head + 1) % v.cap
	if v.count < v.cap {
		v.count++
	}
	v.mu.Unlock()
}

// snapshot returns the last n samples in chronological order.
// Caller must hold v.mu.
func (v *Viewer) snapshot(n int) []float64 {
	if n > v.count {
		n = v.count
	}
	out := make([]float64, n)
	start := (v.head - n + v.cap) % v.cap
	for i := range n {
		out[i] = v.buf[(start+i)%v.cap]
	}
	return out
}

// Run renders the plot at 10 Hz until ctx is cancelled.  Blocks until done.
func (v *Viewer) Run(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	fmt.Print("\033[?25l")                // hide cursor
	defer fmt.Print("\033[?25h\033[2J\033[H") // restore + clear on exit

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			v.draw()
		}
	}
}

// draw renders one frame to stdout.
func (v *Viewer) draw() {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		width, height = 80, 24
	}

	graphW := width - yAxisWidth
	graphH := height - 4 // header + bottom axis + status line + 1 spare

	if graphW < 4 || graphH < 4 {
		return
	}

	v.mu.Lock()
	data := v.snapshot(graphW)
	total := v.count
	v.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("\033[H") // cursor to top-left

	// Inverted header bar.
	fmt.Fprintf(&sb, "\033[7m%-*s\033[0m\n", width, "  Sine Wave Viewer  [Ctrl+C to stop]")

	if len(data) < 2 {
		fmt.Fprintf(&sb, "\n\n  waiting for data...\n")
		fmt.Print(sb.String())
		return
	}

	// Build character grid (row 0 = top = yMax).
	grid := make([][]byte, graphH)
	for i := range graphH {
		row := make([]byte, graphW)
		for j := range graphW {
			row[j] = ' '
		}
		grid[i] = row
	}

	last := data[len(data)-1]
	for col, val := range data {
		norm := (val - yMin) / ySpan
		row := graphH - 1 - int(math.Round(norm*float64(graphH-1)))
		row = max(0, min(row, graphH-1))
		grid[row][col] = '*'
	}

	// Emit rows with Y-axis label on selected rows.
	midRow := graphH / 2
	for i, row := range grid {
		switch i {
		case 0:
			fmt.Fprintf(&sb, "%+6.2f│%s\n", yMax, row)
		case midRow:
			fmt.Fprintf(&sb, "%+6.2f│%s\n", (yMax+yMin)/2, row)
		case graphH - 1:
			fmt.Fprintf(&sb, "%+6.2f│%s\n", yMin, row)
		default:
			fmt.Fprintf(&sb, "      │%s\n", row)
		}
	}

	// Bottom axis and status.
	fmt.Fprintf(&sb, "      └%s\n", strings.Repeat("─", graphW))
	fmt.Fprintf(&sb, "  samples: %-10d  ch0: %+.4f V\n", total, last)

	fmt.Print(sb.String())
}
