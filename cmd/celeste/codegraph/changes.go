// Change impact analysis — maps git diffs to affected symbols and callers.
//
// Given a set of changed files + line ranges from git diff, identifies
// which symbols were directly modified and which callers are transitively
// affected. Produces risk-scored, priority-ordered review guidance.
package codegraph

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ChangedRange represents a modified line range in a file.
type ChangedRange struct {
	File      string
	StartLine int
	EndLine   int
}

// ImpactResult holds the blast radius analysis for a set of changes.
type ImpactResult struct {
	// DirectlyChanged are symbols whose line ranges overlap the diff.
	DirectlyChanged []ImpactSymbol `json:"directly_changed"`
	// AffectedCallers are symbols that call the directly changed symbols.
	AffectedCallers []ImpactSymbol `json:"affected_callers"`
	// UncoveredByTests are changed symbols with no test edges.
	UncoveredByTests []string `json:"uncovered_by_tests"`
	// Summary statistics
	Summary ImpactSummary `json:"summary"`
}

// ImpactSymbol is a symbol with risk metadata.
type ImpactSymbol struct {
	Name      string     `json:"name"`
	Kind      SymbolKind `json:"kind"`
	File      string     `json:"file"`
	Line      int        `json:"line"`
	RiskScore float64    `json:"risk_score"`
}

// ImpactSummary holds aggregate counts.
type ImpactSummary struct {
	FilesChanged    int     `json:"files_changed"`
	SymbolsChanged  int     `json:"symbols_changed"`
	CallersAffected int     `json:"callers_affected"`
	TestGaps        int     `json:"test_gaps"`
	MaxRiskScore    float64 `json:"max_risk_score"`
}

var (
	diffFilePattern = regexp.MustCompile(`^\+\+\+ b/(.+)$`)
	hunkPattern     = regexp.MustCompile(`^@@ .+? \+(\d+)(?:,(\d+))? @@`)
)

// securityKeywords trigger elevated risk scores.
var securityKeywords = []string{
	"auth", "login", "password", "token", "secret", "key",
	"crypt", "hash", "session", "permission", "credential",
	"oauth", "jwt", "csrf", "xss", "inject", "sanitize",
}

// ParseGitDiffRanges runs git diff and extracts changed line ranges per file.
func ParseGitDiffRanges(workspace string, base string) ([]ChangedRange, error) {
	if base == "" {
		base = "HEAD~1"
	}
	cmd := exec.Command("git", "diff", "--unified=0", base, "--")
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	return parseUnifiedDiff(string(out)), nil
}

func parseUnifiedDiff(diff string) []ChangedRange {
	var ranges []ChangedRange
	var currentFile string

	for _, line := range strings.Split(diff, "\n") {
		if m := diffFilePattern.FindStringSubmatch(line); m != nil {
			currentFile = m[1]
			continue
		}
		if m := hunkPattern.FindStringSubmatch(line); m != nil && currentFile != "" {
			start, _ := strconv.Atoi(m[1])
			count := 1
			if m[2] != "" {
				count, _ = strconv.Atoi(m[2])
			}
			end := start + count - 1
			if count == 0 {
				end = start // pure deletion
			}
			ranges = append(ranges, ChangedRange{
				File:      currentFile,
				StartLine: start,
				EndLine:   end,
			})
		}
	}
	return ranges
}

// AnalyzeChanges maps changed line ranges to affected symbols and their callers.
func (idx *Indexer) AnalyzeChanges(base string) (*ImpactResult, error) {
	ranges, err := ParseGitDiffRanges(idx.workspace, base)
	if err != nil {
		return nil, err
	}
	if len(ranges) == 0 {
		return &ImpactResult{}, nil
	}

	// Find directly changed symbols
	changedFiles := make(map[string]bool)
	var directlyChanged []ImpactSymbol
	changedNames := make(map[string]bool)

	for _, r := range ranges {
		changedFiles[r.File] = true
		symbols := idx.store.SymbolsInLineRange(r.File, r.StartLine, r.EndLine)
		for _, sym := range symbols {
			if changedNames[sym.Name] {
				continue
			}
			changedNames[sym.Name] = true
			risk := computeRiskScore(sym.Name, idx.store)
			directlyChanged = append(directlyChanged, ImpactSymbol{
				Name:      sym.Name,
				Kind:      sym.Kind,
				File:      sym.File,
				Line:      sym.Line,
				RiskScore: risk,
			})
		}
	}

	// Find callers of changed symbols (blast radius)
	var affectedCallers []ImpactSymbol
	callerNames := make(map[string]bool)
	for name := range changedNames {
		callers := idx.store.CallersOf(name)
		for _, caller := range callers {
			if changedNames[caller.Name] || callerNames[caller.Name] {
				continue
			}
			callerNames[caller.Name] = true
			risk := computeRiskScore(caller.Name, idx.store)
			affectedCallers = append(affectedCallers, ImpactSymbol{
				Name:      caller.Name,
				Kind:      caller.Kind,
				File:      caller.File,
				Line:      caller.Line,
				RiskScore: risk,
			})
		}
	}

	// Sort by risk score descending
	sort.Slice(directlyChanged, func(i, j int) bool {
		return directlyChanged[i].RiskScore > directlyChanged[j].RiskScore
	})
	sort.Slice(affectedCallers, func(i, j int) bool {
		return affectedCallers[i].RiskScore > affectedCallers[j].RiskScore
	})

	// Test gaps: changed symbols with no test edges
	var testGaps []string
	for _, sym := range directlyChanged {
		if !idx.store.HasTestCoverage(sym.Name) {
			testGaps = append(testGaps, sym.Name)
		}
	}

	var maxRisk float64
	for _, sym := range directlyChanged {
		if sym.RiskScore > maxRisk {
			maxRisk = sym.RiskScore
		}
	}

	return &ImpactResult{
		DirectlyChanged:  directlyChanged,
		AffectedCallers:  affectedCallers,
		UncoveredByTests: testGaps,
		Summary: ImpactSummary{
			FilesChanged:    len(changedFiles),
			SymbolsChanged:  len(directlyChanged),
			CallersAffected: len(affectedCallers),
			TestGaps:        len(testGaps),
			MaxRiskScore:    maxRisk,
		},
	}, nil
}

// computeRiskScore estimates the risk of modifying a symbol.
// Factors: caller count, security keywords, edge density.
func computeRiskScore(name string, store *Store) float64 {
	score := 0.0

	// Caller count: more callers = higher blast radius
	callerCount := store.CallerCount(name)
	score += min64(float64(callerCount)/20.0, 0.30)

	// Security sensitivity
	nameLower := strings.ToLower(name)
	for _, kw := range securityKeywords {
		if strings.Contains(nameLower, kw) {
			score += 0.25
			break
		}
	}

	// Test coverage: untested = higher risk
	if !store.HasTestCoverage(name) {
		score += 0.25
	}

	// Edge count: highly connected = more risk
	edgeCount := store.EdgeCount(name)
	score += min64(float64(edgeCount)/30.0, 0.20)

	if score > 1.0 {
		score = 1.0
	}
	return score
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
