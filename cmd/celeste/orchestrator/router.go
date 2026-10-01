package orchestrator

import (
	"context"
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
	// Notes say which retired models were replaced.
	Notes []string
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
// Each model is resolved against its own endpoint's catalog (the lane's
// base URL and key, else the config's), unless the config pins models; this
// may block on a catalog fetch, bounded by its timeout.
func (r *Router) Resolve(lane TaskLane) (ModelAssignment, error) {
	a := ModelAssignment{Primary: r.cfg.Model}
	if r.cfg.Orchestrator != nil && r.cfg.Orchestrator.Lanes != nil {
		if lc, ok := r.cfg.Orchestrator.Lanes[string(lane)]; ok && strings.TrimSpace(lc.Primary) != "" {
			a = ModelAssignment{
				Primary:         lc.Primary,
				PrimaryBaseURL:  lc.PrimaryBaseURL,
				PrimaryAPIKey:   lc.PrimaryAPIKey,
				Reviewer:        lc.Reviewer,
				ReviewerBaseURL: lc.ReviewerBaseURL,
				ReviewerAPIKey:  lc.ReviewerAPIKey,
			}
		}
	}
	a.Primary = r.served(&a, a.Primary, a.PrimaryBaseURL, a.PrimaryAPIKey)
	if a.HasReviewer() {
		a.Reviewer = r.served(&a, a.Reviewer, a.ReviewerBaseURL, a.ReviewerAPIKey)
	}
	return a, nil
}

// served resolves one lane model on its endpoint, recording any note.
func (r *Router) served(a *ModelAssignment, model, baseURL, apiKey string) string {
	if baseURL == "" {
		baseURL = r.cfg.BaseURL
	}
	if apiKey == "" {
		apiKey = r.cfg.APIKey
	}
	resolved, note := r.cfg.ResolveServedModel(context.Background(), baseURL, apiKey, model)
	if note != "" {
		a.Notes = append(a.Notes, note)
	}
	return resolved
}
