// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file implements an ASCII graph visualization for the code graph.
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
	expanded    map[int]bool // which nodes are expanded to show symbols
	width       int
	height      int
	detail      *graphNode // currently selected node for detail view
	filterKind  string     // filter by symbol kind (empty = all)
	searchQuery string
	viewMode    string // "overview" or "detail"
}

// NewGraphModel creates a graph view from the code graph indexer.
func NewGraphModel(indexer *codegraph.Indexer) GraphModel {
	store := indexer.Store()
	stats, _ := store.Stats()

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

	// Count edges per file
	allFuncs, _ := store.FindAllFunctionsWithEdges()
	for _, f := range allFuncs {
		if n, ok := nodeMap[f.File]; ok {
			n.OutEdges += f.OutEdges
			n.InEdges += f.InEdges
		}
	}

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

	_ = stats // used for header

	return GraphModel{
		nodes:    nodes,
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
		switch msg.String() {
		case "q", "Q", "esc":
			if m.viewMode == "detail" {
				m.viewMode = "overview"
				return m, nil
			}
			return m, nil // parent handles exit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.scroll {
					m.scroll = m.cursor
				}
			}
		case "down", "j":
			if m.cursor < len(m.nodes)-1 {
				m.cursor++
				maxVisible := m.height - 8
				if maxVisible < 5 {
					maxVisible = 20
				}
				if m.cursor >= m.scroll+maxVisible {
					m.scroll++
				}
			}
		case "enter", " ":
			if m.cursor < len(m.nodes) {
				m.expanded[m.cursor] = !m.expanded[m.cursor]
			}
		case "tab":
			// Cycle through filter kinds
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

// glitchChar returns a random corrupted character for decoration.
func glitchChar(i int) string {
	chars := []string{"░", "▒", "▓", "█", "▀", "▄", "▌", "▐", "╫", "╬", "┼", "╳"}
	return chars[i%len(chars)]
}

// View renders the graph visualization.
func (m GraphModel) View() string {
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

	header := fmt.Sprintf(" %s CODE GRAPH %s  %d files  %d symbols  %d edges ",
		glitchChar(0), glitchChar(1), len(m.nodes), totalSyms, totalEdges)
	sb.WriteString(graphHeaderStyle.Width(width).Render(header))
	sb.WriteString("\n")

	// Corruption bar
	bar := strings.Repeat("▀", width)
	sb.WriteString(graphBarStyle.Render(bar))
	sb.WriteString("\n")

	// Filter indicator
	if m.filterKind != "" {
		filterText := fmt.Sprintf("  ◈ Filter: %s  [Tab to change]", m.filterKind)
		sb.WriteString(graphFilterStyle.Render(filterText))
		sb.WriteString("\n")
	}

	// Node list
	maxVisible := m.height - 8
	if maxVisible < 5 {
		maxVisible = 20
	}
	endIdx := m.scroll + maxVisible
	if endIdx > len(m.nodes) {
		endIdx = len(m.nodes)
	}

	for i := m.scroll; i < endIdx; i++ {
		node := m.nodes[i]
		isCursor := i == m.cursor
		isExpanded := m.expanded[i]

		// Count symbols by kind
		kindCounts := make(map[string]int)
		for _, sym := range node.Symbols {
			kindCounts[string(sym.Kind)]++
		}

		// File header line
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

			// Sort by kind then name
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

	// Footer
	sb.WriteString("\n")
	footer := " [↑/↓] Navigate  [Enter] Expand  [Tab] Filter kind  [Q/Esc] Back"
	sb.WriteString(graphFooterStyle.Render(footer))

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
				Foreground(lipgloss.Color("#ff4da6")). // Bright pink
				Background(lipgloss.Color("#1a1a2e"))  // Deep void

	graphBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3a2555")) // Purple border dim

	graphNodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#b8afc8")) // Secondary text

	graphCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ff4da6")). // Bright pink cursor
				Bold(true).
				Background(lipgloss.Color("#1a1a2e")) // Subtle highlight

	graphHotStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c084fc")). // Neon purple for hot files
			Bold(true)

	graphSymbolStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b5cf6")) // Purple for symbols

	graphMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7a7085")) // Muted

	graphFilterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00d4ff")) // Cyan accent

	graphFooterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5a4575")) // Dim purple
)
