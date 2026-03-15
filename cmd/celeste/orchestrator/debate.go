package orchestrator

// DebateRole identifies which side is speaking in a review debate.
type DebateRole int

const (
	RoleReviewer DebateRole = iota
	RolePrimary
)

// VerdictKind is the outcome of a review debate.
type VerdictKind int

const (
	VerdictApproved  VerdictKind = iota
	VerdictNeedsWork
	VerdictContested
)

// Issue is a code issue raised by the reviewer.
type Issue struct {
	File        string
	Line        int
	Severity    string // "low", "medium", "high"
	Description string
}

// DebateTurn is one side's contribution to a debate round.
type DebateTurn struct {
	Round  int
	Role   DebateRole
	Input  string
	Output string
}

// DebateResult is the final outcome of all debate rounds.
type DebateResult struct {
	Kind   VerdictKind
	Issues []Issue
	Score  float64 // 0.0–1.0; higher = cleaner code
}

// DebateOptions configures a DebateManager.
type DebateOptions struct {
	MaxRounds int // default 3
}

// DebateManager tracks debate turns and produces a verdict.
type DebateManager struct {
	opts  DebateOptions
	turns []DebateTurn
}

// NewDebateManager creates a DebateManager with the given options.
func NewDebateManager(opts DebateOptions) *DebateManager {
	if opts.MaxRounds <= 0 {
		opts.MaxRounds = 3
	}
	return &DebateManager{opts: opts}
}

// MaxRounds returns the configured maximum debate rounds.
func (d *DebateManager) MaxRounds() int { return d.opts.MaxRounds }

// Turns returns all turns added so far.
func (d *DebateManager) Turns() []DebateTurn { return d.turns }

// AddTurn appends a turn to the debate.
func (d *DebateManager) AddTurn(turn DebateTurn) {
	d.turns = append(d.turns, turn)
}

// RoundsCompleted returns the number of full rounds (reviewer + primary) completed.
func (d *DebateManager) RoundsCompleted() int {
	return len(d.turns) / 2
}

// Verdict produces a DebateResult from the given open issues.
func (d *DebateManager) Verdict(issues []Issue) DebateResult {
	switch {
	case len(issues) == 0:
		return DebateResult{Kind: VerdictApproved, Issues: issues, Score: 0.95}
	case d.RoundsCompleted() >= d.opts.MaxRounds:
		return DebateResult{Kind: VerdictContested, Issues: issues, Score: 0.5}
	default:
		// Score degrades with number and severity of issues.
		score := 0.75
		for _, iss := range issues {
			switch iss.Severity {
			case "high":
				score -= 0.15
			case "medium":
				score -= 0.08
			default:
				score -= 0.03
			}
		}
		if score < 0.1 {
			score = 0.1
		}
		return DebateResult{Kind: VerdictNeedsWork, Issues: issues, Score: score}
	}
}
