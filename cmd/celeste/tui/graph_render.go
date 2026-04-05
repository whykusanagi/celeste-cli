package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
)

// cellStyle tags grid cells for colored rendering.
type cellStyle int

const (
	styleEmpty cellStyle = iota
	styleEdge
	styleNode
	styleLabel
	styleGlitch
)

// RenderCodeGraphConstellation produces an ASCII constellation-style visualization
// of the package dependency graph using the corrupted theme palette.
func RenderCodeGraphConstellation(indexer *codegraph.Indexer, width int) string {
	if width < 40 {
		width = 80
	}

	stats, err := indexer.Stats()
	if err != nil {
		return ""
	}

	packages, edges, _ := indexer.PackageGraph()
	if len(packages) == 0 {
		return ""
	}

	// Limit to top packages for readability
	maxNodes := 16
	if len(packages) < maxNodes {
		maxNodes = len(packages)
	}
	packages = packages[:maxNodes]

	// Build adjacency set for the visible packages
	pkgSet := make(map[string]bool)
	for _, p := range packages {
		pkgSet[p.Name] = true
	}
	var visibleEdges []codegraph.PackageEdge
	for _, e := range edges {
		if pkgSet[e.Source] && pkgSet[e.Target] {
			visibleEdges = append(visibleEdges, e)
		}
	}

	// Layout: place nodes in a circular-ish pattern on a grid
	gridW := width - 4
	gridH := maxNodes + 4
	if gridH > 20 {
		gridH = 20
	}
	if gridH < 8 {
		gridH = 8
	}

	type nodePos struct {
		x, y int
		pkg  codegraph.PackageInfo
	}

	// Place nodes in an elliptical layout
	centerX := gridW / 2
	centerY := gridH / 2
	radiusX := float64(gridW) * 0.38
	radiusY := float64(gridH) * 0.42

	nodes := make([]nodePos, len(packages))
	for i, pkg := range packages {
		angle := 2.0 * math.Pi * float64(i) / float64(len(packages))
		// Offset angle so the largest package is at top
		angle -= math.Pi / 2.0
		x := centerX + int(radiusX*math.Cos(angle))
		y := centerY + int(radiusY*math.Sin(angle))
		// Clamp to grid
		if x < 2 {
			x = 2
		}
		if x >= gridW-2 {
			x = gridW - 3
		}
		if y < 0 {
			y = 0
		}
		if y >= gridH {
			y = gridH - 1
		}
		nodes[i] = nodePos{x: x, y: y, pkg: pkg}
	}

	// Build grid
	grid := make([][]rune, gridH)
	for i := range grid {
		grid[i] = make([]rune, gridW)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	styles := make([][]cellStyle, gridH)
	for i := range styles {
		styles[i] = make([]cellStyle, gridW)
	}

	// Draw edges first (so nodes overlay them)
	nodeIndex := make(map[string]int)
	for i, n := range nodes {
		nodeIndex[n.pkg.Name] = i
	}

	for _, e := range visibleEdges {
		si, ok1 := nodeIndex[e.Source]
		ti, ok2 := nodeIndex[e.Target]
		if !ok1 || !ok2 {
			continue
		}
		// Draw line between nodes using Bresenham-ish
		drawLine(grid, styles, nodes[si].x, nodes[si].y, nodes[ti].x, nodes[ti].y)
	}

	// Draw nodes (overwrite edges at node positions)
	for _, n := range nodes {
		if n.y >= 0 && n.y < gridH && n.x >= 0 && n.x < gridW {
			// Node marker
			grid[n.y][n.x] = '◈'
			styles[n.y][n.x] = styleNode

			// Short label (truncate package name)
			label := shortPkgName(n.pkg.Name)
			labelStart := n.x + 2
			if labelStart+len(label) >= gridW {
				labelStart = n.x - len(label) - 1
			}
			if labelStart < 0 {
				labelStart = 0
			}
			for j, ch := range label {
				pos := labelStart + j
				if pos >= 0 && pos < gridW && grid[n.y][pos] == ' ' {
					grid[n.y][pos] = ch
					styles[n.y][pos] = styleLabel
				}
			}
		}
	}

	// Add scattered glitch characters in empty spaces
	glitchChars := []rune{'░', '·', '∙', '⋅'}
	for y := 0; y < gridH; y++ {
		for x := 0; x < gridW; x++ {
			if grid[y][x] == ' ' {
				// Sparse random glitch based on position hash
				h := (x*7 + y*13 + x*y) % 47
				if h == 0 || h == 1 {
					grid[y][x] = glitchChars[h%len(glitchChars)]
					styles[y][x] = styleGlitch
				}
			}
		}
	}

	// Render with colors
	var sb strings.Builder

	// Top border
	topBar := "  " + strings.Repeat("▀", gridW)
	sb.WriteString(constBarStyle.Render(topBar))
	sb.WriteString("\n")

	// Title
	title := fmt.Sprintf("  ░▒▓ CODE GRAPH ▓▒░  %d packages · %d symbols · %d edges",
		len(packages), stats.TotalSymbols, stats.TotalEdges)
	sb.WriteString(constTitleStyle.Render(title))
	sb.WriteString("\n\n")

	// Render grid rows
	for y := 0; y < gridH; y++ {
		sb.WriteString("  ") // left margin
		for x := 0; x < gridW; x++ {
			ch := string(grid[y][x])
			switch styles[y][x] {
			case styleNode:
				sb.WriteString(constNodeStyle.Render(ch))
			case styleEdge:
				sb.WriteString(constEdgeStyle.Render(ch))
			case styleLabel:
				sb.WriteString(constLabelStyle.Render(ch))
			case styleGlitch:
				sb.WriteString(constGlitchStyle.Render(ch))
			default:
				sb.WriteString(ch)
			}
		}
		sb.WriteString("\n")
	}

	// Bottom border
	botBar := "  " + strings.Repeat("▄", gridW)
	sb.WriteString(constBarStyle.Render(botBar))
	sb.WriteString("\n")

	// Stats line
	statsLine := fmt.Sprintf("  Files: %d │ Symbols: %d │ Edges: %d │ Packages: %d",
		stats.TotalFiles, stats.TotalSymbols, stats.TotalEdges, len(packages))
	sb.WriteString(constStatsStyle.Render(statsLine))
	sb.WriteString("\n\n")

	// Call to action
	sb.WriteString(constCTAStyle.Render("  /graph"))
	sb.WriteString(constCTADimStyle.Render(" to explore interactively"))

	return sb.String()
}

// drawLine draws a line between two points on the grid using simple stepping.
func drawLine(grid [][]rune, styles [][]cellStyle, x0, y0, x1, y1 int) {
	dx := x1 - x0
	dy := y1 - y0
	steps := abs(dx)
	if abs(dy) > steps {
		steps = abs(dy)
	}
	if steps == 0 {
		return
	}

	for i := 1; i < steps; i++ { // skip endpoints (nodes will draw there)
		x := x0 + dx*i/steps
		y := y0 + dy*i/steps
		if y >= 0 && y < len(grid) && x >= 0 && x < len(grid[y]) {
			if grid[y][x] == ' ' || grid[y][x] == '░' || grid[y][x] == '·' {
				// Choose line character based on angle
				if abs(dx) > abs(dy)*2 {
					grid[y][x] = '─'
				} else if abs(dy) > abs(dx)*2 {
					grid[y][x] = '│'
				} else {
					grid[y][x] = '╌'
				}
				styles[y][x] = styleEdge
			}
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// shortPkgName extracts a short display name from a package path.
func shortPkgName(pkg string) string {
	// Take last component of path
	parts := strings.Split(pkg, "/")
	name := parts[len(parts)-1]
	if len(name) > 12 {
		name = name[:12]
	}
	return name
}

// Corrupted-theme constellation styles
var (
	constTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ff4da6")) // Bright pink

	constBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3a2555")) // Purple border dim

	constNodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff4da6")). // Bright pink nodes
			Bold(true)

	constEdgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5a4575")) // Dim purple edges

	constLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c084fc")) // Neon purple labels

	constGlitchStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#2a1545")) // Very dim purple glitch

	constStatsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b5cf6")) // Purple stats

	constCTAStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff4da6")). // Pink CTA
			Bold(true)

	constCTADimStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7a7085")) // Muted
)
