// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file implements an interactive code graph browser for the code graph.
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
)

// graphNode represents a file/package cluster in the graph view.
type graphNode struct {
	File     string
	Package  string
	Symbols  []codegraph.Symbol
	OutEdges int // total outgoing edges from this file
	InEdges  int // total incoming edges to this file
}

// graphEdge represents a connection between two file clusters.
type graphEdge struct {
	SourceFile string
	TargetFile string
	Count      int
}

// GraphModel is the TUI model for the interactive code graph viewer.
type GraphModel struct {
	nodes       []graphNode
	edges       []graphEdge
	cursor      int
	scroll      int
	expanded    map[int]bool   // which nodes are expanded to show symbols
	width       int
	height      int
	filterKind  string         // filter by symbol kind (empty = all)
	searchQuery string         // filter nodes by name
	searching   bool           // true when search input is active
	viewMode    string         // "overview" or "detail"
	detailIdx   int            // index of node shown in detail view
}

// NewGraphModel creates a graph view from the code graph indexer.
func NewGraphModel(indexer *codegraph.Indexer) GraphModel {
	store := indexer.Store()

	// Build file clusters
	files, _ := store.GetAllFiles()
	nodeMap := make(map[string]*graphNode)
	for _, f := range files {
		nodeMap[f.Path] = &graphNode{
			File:    f.Path,
			Package: f.Language,
		}
	}

	// Populate symbols per file
	for path, node := range nodeMap {
		syms, _ := store.GetSymbolsByFile(path)
		node.Symbols = syms
	}

	// Count edges per file and collect inter-file edges
	allFuncs, _ := store.FindAllFunctionsWithEdges()
	for _, f := range allFuncs {
		if n, ok := nodeMap[f.File]; ok {
			n.OutEdges += f.OutEdges
			n.InEdges += f.InEdges
		}
	}

	// Build inter-file edge map from the graph
	edgeCount := make(map[string]int) // "src→dst" -> count
	// We can approximate edges by looking at symbols that share edges across files
	// For now, use package-level edges as a proxy
	pkgs, pkgEdges, _ := indexer.PackageGraph()
	_ = pkgs
	var edges []graphEdge
	for _, pe := range pkgEdges {
		edges = append(edges, graphEdge{
			SourceFile: pe.Source,
			TargetFile: pe.Target,
			Count:      pe.Count,
		})
	}
	_ = edgeCount

	// Convert to sorted slices — sort by edge count (most connected first)
	var nodes []graphNode
	for _, n := range nodeMap {
		if len(n.Symbols) > 0 {
			nodes = append(nodes, *n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		totalI := nodes[i].OutEdges + nodes[i].InEdges
		totalJ := nodes[j].OutEdges + nodes[j].InEdges
		if totalI != totalJ {
			return totalI > totalJ
		}
		return nodes[i].File < nodes[j].File
	})

	return GraphModel{
		nodes:    nodes,
		edges:    edges,
		expanded: make(map[int]bool),
		viewMode: "overview",
	}
}

// Init initializes the graph model.
func (m GraphModel) Init() tea.Cmd {
	return nil
}

// Update handles input for the graph view.
func (m GraphModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Search mode input
		if m.searching {
			switch msg.String() {
			case "enter", "esc":
				m.searching = false
			case "backspace":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				}
			default:
				if len(msg.String()) == 1 {
					m.searchQuery += msg.String()
				}
			}
			m.cursor = 0
			m.scroll = 0
			return m, nil
		}

		switch msg.String() {
		case "q", "Q", "esc":
			if m.viewMode == "detail" {
				m.viewMode = "overview"
				return m, nil
			}
			if m.searchQuery != "" {
				m.searchQuery = ""
				return m, nil
			}
			return m, nil // parent handles exit
		case "up", "k":
			visible := m.visibleNodes()
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.scroll {
					m.scroll = m.cursor
				}
			}
			_ = visible
		case "down", "j":
			visible := m.visibleNodes()
			if m.cursor < len(visible)-1 {
				m.cursor++
				maxVis := m.maxVisible()
				if m.cursor >= m.scroll+maxVis {
					m.scroll++
				}
			}
		case "enter", " ":
			if m.viewMode == "detail" {
				// In detail mode, enter does nothing extra
				return m, nil
			}
			visible := m.visibleNodes()
			if m.cursor < len(visible) {
				// Toggle between expand and detail
				idx := visible[m.cursor].origIdx
				m.expanded[idx] = !m.expanded[idx]
			}
		case "d":
			// Detail view: show connections for selected node
			visible := m.visibleNodes()
			if m.cursor < len(visible) {
				m.detailIdx = visible[m.cursor].origIdx
				m.viewMode = "detail"
			}
		case "/":
			m.searching = true
		case "tab":
			kinds := []string{"", "function", "method", "struct", "interface", "class"}
			for i, k := range kinds {
				if k == m.filterKind {
					m.filterKind = kinds[(i+1)%len(kinds)]
					break
				}
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

type visibleNode struct {
	node    graphNode
	origIdx int
}

func (m GraphModel) visibleNodes() []visibleNode {
	var visible []visibleNode
	for i, node := range m.nodes {
		if m.searchQuery != "" {
			if !strings.Contains(strings.ToLower(node.File), strings.ToLower(m.searchQuery)) {
				continue
			}
		}
		visible = append(visible, visibleNode{node: node, origIdx: i})
	}
	return visible
}

func (m GraphModel) maxVisible() int {
	v := m.height - 8
	if v < 5 {
		v = 20
	}
	return v
}

// glitchChar returns a corrupted character for decoration.
func glitchChar(i int) string {
	chars := []string{"░", "▒", "▓", "█", "▀", "▄", "▌", "▐", "╫", "╬", "┼", "╳"}
	return chars[i%len(chars)]
}

// View renders the graph visualization.
func (m GraphModel) View() string {
	if m.viewMode == "detail" {
		return m.renderDetail()
	}
	return m.renderOverview()
}

func (m GraphModel) renderOverview() string {
	if len(m.nodes) == 0 {
		return graphHeaderStyle.Render("No code graph data. Run `celeste index` first.")
	}

	var sb strings.Builder
	width := m.width
	if width < 40 {
		width = 80
	}

	// Header with stats
	totalSyms := 0
	totalEdges := 0
	for _, n := range m.nodes {
		totalSyms += len(n.Symbols)
		totalEdges += n.OutEdges
	}

	header := fmt.Sprintf(" %s CODE GRAPH %s  %d files  %d symbols  %d edges  %d pkg connections",
		glitchChar(0), glitchChar(1), len(m.nodes), totalSyms, totalEdges, len(m.edges))
	sb.WriteString(graphHeaderStyle.Width(width).Render(header))
	sb.WriteString("\n")

	bar := strings.Repeat("▀", width)
	sb.WriteString(graphBarStyle.Render(bar))
	sb.WriteString("\n")

	// Search indicator
	if m.searching {
		sb.WriteString(graphFilterStyle.Render(fmt.Sprintf("  🔍 Search: %s_", m.searchQuery)))
		sb.WriteString("\n")
	} else if m.searchQuery != "" {
		sb.WriteString(graphFilterStyle.Render(fmt.Sprintf("  🔍 Filter: %s  [/ to search, Esc to clear]", m.searchQuery)))
		sb.WriteString("\n")
	}

	// Filter indicator
	if m.filterKind != "" {
		sb.WriteString(graphFilterStyle.Render(fmt.Sprintf("  ◈ Kind: %s  [Tab to change]", m.filterKind)))
		sb.WriteString("\n")
	}

	// Node list
	visible := m.visibleNodes()
	maxVis := m.maxVisible()
	endIdx := m.scroll + maxVis
	if endIdx > len(visible) {
		endIdx = len(visible)
	}

	for vi := m.scroll; vi < endIdx; vi++ {
		vn := visible[vi]
		node := vn.node
		origIdx := vn.origIdx
		isCursor := vi == m.cursor
		isExpanded := m.expanded[origIdx]

		kindCounts := make(map[string]int)
		for _, sym := range node.Symbols {
			kindCounts[string(sym.Kind)]++
		}

		connStr := ""
		total := node.OutEdges + node.InEdges
		if total > 0 {
			connStr = fmt.Sprintf(" ⇄%d", total)
		}

		symCount := len(node.Symbols)
		if m.filterKind != "" {
			symCount = kindCounts[m.filterKind]
		}

		expandIcon := "▸"
		if isExpanded {
			expandIcon = "▾"
		}

		line := fmt.Sprintf(" %s %-50s  %d syms%s",
			expandIcon, truncate(node.File, 50), symCount, connStr)

		if isCursor {
			sb.WriteString(graphCursorStyle.Width(width - 2).Render(line))
		} else if total > 20 {
			sb.WriteString(graphHotStyle.Render(line))
		} else {
			sb.WriteString(graphNodeStyle.Render(line))
		}
		sb.WriteString("\n")

		// Expanded: show symbols
		if isExpanded {
			symbols := node.Symbols
			if m.filterKind != "" {
				var filtered []codegraph.Symbol
				for _, s := range symbols {
					if string(s.Kind) == m.filterKind {
						filtered = append(filtered, s)
					}
				}
				symbols = filtered
			}

			sort.Slice(symbols, func(a, b int) bool {
				if symbols[a].Kind != symbols[b].Kind {
					return symbols[a].Kind < symbols[b].Kind
				}
				return symbols[a].Name < symbols[b].Name
			})

			for j, sym := range symbols {
				if j >= 25 {
					remaining := len(symbols) - 25
					sb.WriteString(graphMutedStyle.Render(
						fmt.Sprintf("    ╰─ ... and %d more", remaining)))
					sb.WriteString("\n")
					break
				}
				kindIcon := symbolIcon(string(sym.Kind))
				connector := "├─"
				if j == len(symbols)-1 || j == 24 {
					connector = "╰─"
				}
				symLine := fmt.Sprintf("    %s %s %s :%d",
					connector, kindIcon, sym.Name, sym.Line)
				sb.WriteString(graphSymbolStyle.Render(symLine))
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString("\n")
	footer := " [↑/↓] Navigate  [Enter] Expand  [D] Detail  [/] Search  [Tab] Filter  [Q/Esc] Back"
	sb.WriteString(graphFooterStyle.Render(footer))

	return sb.String()
}

// renderDetail shows the selected node with its connections.
func (m GraphModel) renderDetail() string {
	if m.detailIdx >= len(m.nodes) {
		return graphHeaderStyle.Render("Invalid node selection")
	}

	node := m.nodes[m.detailIdx]
	var sb strings.Builder
	width := m.width
	if width < 40 {
		width = 80
	}

	// Header
	header := fmt.Sprintf(" %s %s %s", glitchChar(2), node.File, glitchChar(3))
	sb.WriteString(graphHeaderStyle.Width(width).Render(header))
	sb.WriteString("\n")
	sb.WriteString(graphBarStyle.Render(strings.Repeat("▀", width)))
	sb.WriteString("\n\n")

	// Stats
	sb.WriteString(graphFilterStyle.Render(fmt.Sprintf("  Symbols: %d  │  Outgoing: %d  │  Incoming: %d",
		len(node.Symbols), node.OutEdges, node.InEdges)))
	sb.WriteString("\n\n")

	// Symbols list
	sb.WriteString(graphHotStyle.Render("  Symbols:"))
	sb.WriteString("\n")
	for i, sym := range node.Symbols {
		if i >= 30 {
			sb.WriteString(graphMutedStyle.Render(fmt.Sprintf("    ... and %d more", len(node.Symbols)-30)))
			sb.WriteString("\n")
			break
		}
		icon := symbolIcon(string(sym.Kind))
		line := fmt.Sprintf("    %s %s %s :%d", icon, sym.Name, graphMutedStyle.Render(string(sym.Kind)), sym.Line)
		sb.WriteString(graphSymbolStyle.Render(line))
		sb.WriteString("\n")
	}

	// Connected packages (from edges)
	sb.WriteString("\n")
	sb.WriteString(graphHotStyle.Render("  Package Connections:"))
	sb.WriteString("\n")

	// Find this file's package
	filePkg := ""
	for _, sym := range node.Symbols {
		if sym.Package != "" {
			filePkg = sym.Package
			break
		}
	}

	connCount := 0
	if filePkg != "" {
		for _, edge := range m.edges {
			if edge.SourceFile == filePkg {
				sb.WriteString(graphSymbolStyle.Render(fmt.Sprintf("    → %s (%d calls)", edge.TargetFile, edge.Count)))
				sb.WriteString("\n")
				connCount++
			} else if edge.TargetFile == filePkg {
				sb.WriteString(graphSymbolStyle.Render(fmt.Sprintf("    ← %s (%d calls)", edge.SourceFile, edge.Count)))
				sb.WriteString("\n")
				connCount++
			}
		}
	}
	if connCount == 0 {
		sb.WriteString(graphMutedStyle.Render("    No cross-package connections"))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(graphFooterStyle.Render(" [Q/Esc] Back to overview"))

	return sb.String()
}

func symbolIcon(kind string) string {
	switch kind {
	case "function":
		return "ƒ"
	case "method":
		return "λ"
	case "struct":
		return "◆"
	case "class":
		return "◈"
	case "interface":
		return "◇"
	case "type":
		return "τ"
	case "const":
		return "π"
	case "var":
		return "ν"
	case "import":
		return "⇢"
	default:
		return "·"
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max+3:]
}

// Corrupted-theme graph styles
var (
	graphHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ff4da6")).
				Background(lipgloss.Color("#1a1a2e"))

	graphBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3a2555"))

	graphNodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#b8afc8"))

	graphCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ff4da6")).
				Bold(true).
				Background(lipgloss.Color("#1a1a2e"))

	graphHotStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c084fc")).
			Bold(true)

	graphSymbolStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b5cf6"))

	graphMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7a7085"))

	graphFilterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00d4ff"))

	graphFooterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5a4575"))
)
