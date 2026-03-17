package orchestrator

import (
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// ModelAssignment describes which models to use for a given run.
// Per-role BaseURL and APIKey allow cross-provider orchestration.
type ModelAssignment struct {
	Primary         string
	PrimaryBaseURL  string
	PrimaryAPIKey   string
	Reviewer        string
	ReviewerBaseURL string
	ReviewerAPIKey  string
}

// HasReviewer returns true when a non-blank reviewer model is assigned.
func (m ModelAssignment) HasReviewer() bool {
	return strings.TrimSpace(m.Reviewer) != ""
}

// Router maps TaskLanes to ModelAssignments using the user's config.
type Router struct {
	cfg *config.Config
}

// NewRouter creates a Router backed by the given config.
func NewRouter(cfg *config.Config) *Router {
	return &Router{cfg: cfg}
}

// Resolve returns the ModelAssignment for the given lane.
// Falls back to cfg.Model as primary with no reviewer when the lane is unconfigured.
func (r *Router) Resolve(lane TaskLane) (ModelAssignment, error) {
	if r.cfg.Orchestrator != nil && r.cfg.Orchestrator.Lanes != nil {
		if lc, ok := r.cfg.Orchestrator.Lanes[string(lane)]; ok && strings.TrimSpace(lc.Primary) != "" {
			return ModelAssignment{
				Primary:         lc.Primary,
				PrimaryBaseURL:  lc.PrimaryBaseURL,
				PrimaryAPIKey:   lc.PrimaryAPIKey,
				Reviewer:        lc.Reviewer,
				ReviewerBaseURL: lc.ReviewerBaseURL,
				ReviewerAPIKey:  lc.ReviewerAPIKey,
			}, nil
		}
	}
	return ModelAssignment{Primary: r.cfg.Model}, nil
}
