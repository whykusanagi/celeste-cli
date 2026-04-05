package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
)

// RenderCodeGraphConstellation produces a structured dependency tree visualization
// of the code graph using the corrupted theme palette.
func RenderCodeGraphConstellation(indexer *codegraph.Indexer, width int) string {
	if width < 40 {
		width = 80
	}

	stats, err := indexer.Stats()
	if err != nil {
		return ""
	}

	// Try package-level graph first, fall back to file-level
	packages, pkgEdges, _ := indexer.PackageGraph()

	type nodeInfo struct {
		Name string
		Size int
	}
	type edgeInfo struct {
		Source string
		Target string
	}

	var nodes []nodeInfo
	var graphEdges []edgeInfo

	if len(packages) > 0 && len(pkgEdges) > 0 {
		for _, p := range packages {
			nodes = append(nodes, nodeInfo{Name: p.Name, Size: p.SymbolCount})
		}
		for _, e := range pkgEdges {
			graphEdges = append(graphEdges, edgeInfo{Source: e.Source, Target: e.Target})
		}
	} else {
		fileEdges, _ := indexer.Store().GetFileGraph()
		if len(fileEdges) == 0 {
			return renderTextStats(stats, indexer)
		}

		fileWeight := make(map[string]int)
		for _, e := range fileEdges {
			fileWeight[e.Source] += e.Count
			fileWeight[e.Target] += e.Count
			graphEdges = append(graphEdges, edgeInfo{Source: shortName(e.Source), Target: shortName(e.Target)})
		}

		type fw struct {
			file   string
			weight int
		}
		var sorted []fw
		for f, w := range fileWeight {
			sorted = append(sorted, fw{f, w})
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].weight > sorted[j].weight })

		for _, s := range sorted {
			nodes = append(nodes, nodeInfo{Name: shortName(s.file), Size: s.weight})
		}
	}

	if len(nodes) == 0 {
		return renderTextStats(stats, indexer)
	}

	maxNodes := 16
	if len(nodes) < maxNodes {
		maxNodes = len(nodes)
	}
	nodes = nodes[:maxNodes]

	// Build adjacency
	nodeSet := make(map[string]bool)
	for _, n := range nodes {
		nodeSet[n.Name] = true
	}
	nameToIdx := make(map[string]int)
	for i, n := range nodes {
		nameToIdx[n.Name] = i
	}

	connections := make(map[int][]int)
	visibleCount := 0
	for _, e := range graphEdges {
		if !nodeSet[e.Source] || !nodeSet[e.Target] {
			continue
		}
		si := nameToIdx[e.Source]
		ti := nameToIdx[e.Target]
		connections[si] = append(connections[si], ti)
		visibleCount++
	}

	// Render as dependency tree with size bars
	var sb strings.Builder

	topBar := "  " + strings.Repeat("▀", width-4)
	sb.WriteString(constBarStyle.Render(topBar))
	sb.WriteString("\n")

	title := fmt.Sprintf("  ░▒▓ CODE GRAPH ▓▒░  %d nodes · %d symbols · %d connections",
		len(nodes), stats.TotalSymbols, visibleCount)
	sb.WriteString(constTitleStyle.Render(title))
	sb.WriteString("\n\n")

	// Find max size for scaling bars
	maxSize := 1
	for _, n := range nodes {
		if n.Size > maxSize {
			maxSize = n.Size
		}
	}

	for i, n := range nodes {
		// Size bar — scaled to max
		barLen := n.Size * 20 / maxSize
		if barLen < 1 {
			barLen = 1
		}
		sizeBar := strings.Repeat("█", barLen)

		// Node line
		sb.WriteString(constNodeStyle.Render(fmt.Sprintf("  ◈ %-18s", n.Name)))
		sb.WriteString(constEdgeStyle.Render(sizeBar))
		sb.WriteString(constGlitchStyle.Render(fmt.Sprintf(" %d", n.Size)))
		sb.WriteString("\n")

		// Connections
		conns := connections[i]
		if len(conns) > 0 {
			for ci, targetIdx := range conns {
				connector := "  ├─▸ "
				if ci == len(conns)-1 {
					connector = "  ╰─▸ "
				}
				sb.WriteString(constEdgeStyle.Render(connector))
				sb.WriteString(constLabelStyle.Render(nodes[targetIdx].Name))
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString("\n")
	botBar := "  " + strings.Repeat("▄", width-4)
	sb.WriteString(constBarStyle.Render(botBar))
	sb.WriteString("\n")

	statsLine := fmt.Sprintf("  Files: %d │ Symbols: %d │ Edges: %d",
		stats.TotalFiles, stats.TotalSymbols, stats.TotalEdges)
	sb.WriteString(constStatsStyle.Render(statsLine))
	sb.WriteString("\n\n")

	sb.WriteString(constCTAStyle.Render("  /graph"))
	sb.WriteString(constCTADimStyle.Render(" to explore interactively"))

	return sb.String()
}

// shortName extracts a short display name from a file path.
func shortName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return path
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// renderTextStats shows a styled text summary when there are no edges.
func renderTextStats(stats *codegraph.StoreStats, indexer *codegraph.Indexer) string {
	var sb strings.Builder

	sb.WriteString(constTitleStyle.Render("  ░▒▓ CODE GRAPH ▓▒░"))
	sb.WriteString("\n\n")

	sb.WriteString(constStatsStyle.Render(fmt.Sprintf("  Files: %d │ Symbols: %d │ Edges: %d",
		stats.TotalFiles, stats.TotalSymbols, stats.TotalEdges)))
	sb.WriteString("\n\n")

	if len(stats.SymbolsByKind) > 0 {
		sb.WriteString(constLabelStyle.Render("  Symbols by kind:"))
		sb.WriteString("\n")
		for kind, count := range stats.SymbolsByKind {
			sb.WriteString(constStatsStyle.Render(fmt.Sprintf("    %s %s: %d", symbolIcon(string(kind)), kind, count)))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	files, _ := indexer.Store().GetAllFiles()
	type fileInfo struct {
		path  string
		count int
	}
	var topFiles []fileInfo
	for _, f := range files {
		syms, _ := indexer.Store().GetSymbolsByFile(f.Path)
		if len(syms) > 0 {
			topFiles = append(topFiles, fileInfo{f.Path, len(syms)})
		}
	}
	sort.Slice(topFiles, func(i, j int) bool { return topFiles[i].count > topFiles[j].count })

	if len(topFiles) > 0 {
		sb.WriteString(constLabelStyle.Render("  Top files:"))
		sb.WriteString("\n")
		limit := 10
		if len(topFiles) < limit {
			limit = len(topFiles)
		}
		for _, f := range topFiles[:limit] {
			sb.WriteString(constStatsStyle.Render(fmt.Sprintf("    ◈ %-45s %d syms", f.path, f.count)))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(constCTAStyle.Render("  /graph"))
	sb.WriteString(constCTADimStyle.Render(" to explore interactively"))

	return sb.String()
}

// Corrupted-theme styles
var (
	constTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ff4da6"))

	constBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3a2555"))

	constNodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff4da6")).
			Bold(true)

	constEdgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5a4575"))

	constLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c084fc"))

	constGlitchStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#2a1545"))

	constStatsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b5cf6"))

	constCTAStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff4da6")).
			Bold(true)

	constCTADimStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7a7085"))
)
