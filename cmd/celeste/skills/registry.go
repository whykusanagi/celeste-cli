// Package skills provides the skill registry and execution system.
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a skill definition loaded from JSON.
type Skill struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Handler     string                 `json:"handler,omitempty"` // Not used in Go, skills are built-in
}

// Registry manages skill definitions and execution.
type Registry struct {
	skills    map[string]Skill
	handlers  map[string]SkillHandler
	skillsDir string
}

// SkillHandler is a function that executes a skill.
type SkillHandler func(args map[string]interface{}) (interface{}, error)

// NewRegistry creates a new skill registry.
func NewRegistry() *Registry {
	homeDir, _ := os.UserHomeDir()
	skillsDir := filepath.Join(homeDir, ".celeste", "skills")

	return &Registry{
		skills:    make(map[string]Skill),
		handlers:  make(map[string]SkillHandler),
		skillsDir: skillsDir,
	}
}

// SetSkillsDir sets the directory to load skills from.
func (r *Registry) SetSkillsDir(dir string) {
	r.skillsDir = dir
}

// RegisterHandler registers a handler function for a skill.
func (r *Registry) RegisterHandler(name string, handler SkillHandler) {
	r.handlers[name] = handler
}

// LoadSkills loads all skill definitions from the skills directory.
func (r *Registry) LoadSkills() error {
	// Ensure directory exists
	if err := os.MkdirAll(r.skillsDir, 0755); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}

	// Find all JSON files
	files, err := filepath.Glob(filepath.Join(r.skillsDir, "*.json"))
	if err != nil {
		return fmt.Errorf("failed to list skill files: %w", err)
	}

	// Load each skill file
	for _, file := range files {
		if err := r.loadSkillFile(file); err != nil {
			// Log warning but continue loading other skills
			fmt.Fprintf(os.Stderr, "Warning: failed to load skill %s: %v\n", file, err)
			continue
		}
	}

	return nil
}

// loadSkillFile loads a single skill definition from a JSON file.
func (r *Registry) loadSkillFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var skill Skill
	if err := json.Unmarshal(data, &skill); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	if skill.Name == "" {
		return fmt.Errorf("skill name is required")
	}

	r.skills[skill.Name] = skill
	return nil
}

// RegisterSkill manually registers a skill definition.
func (r *Registry) RegisterSkill(skill Skill) {
	r.skills[skill.Name] = skill
}

// GetSkill returns a skill by name.
func (r *Registry) GetSkill(name string) (Skill, bool) {
	skill, ok := r.skills[name]
	return skill, ok
}

// GetAllSkills returns all registered skills.
func (r *Registry) GetAllSkills() []Skill {
	skills := make([]Skill, 0, len(r.skills))
	for _, skill := range r.skills {
		skills = append(skills, skill)
	}
	return skills
}

// GetToolDefinitions returns skills in OpenAI tool format for API calls.
func (r *Registry) GetToolDefinitions() []map[string]interface{} {
	tools := make([]map[string]interface{}, 0, len(r.skills))
	for _, skill := range r.skills {
		tool := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        skill.Name,
				"description": skill.Description,
				"parameters":  skill.Parameters,
			},
		}
		tools = append(tools, tool)
	}
	return tools
}

// Execute runs a skill by name with the given arguments.
func (r *Registry) Execute(name string, args map[string]interface{}) (interface{}, error) {
	// Check if skill exists
	_, ok := r.skills[name]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", name)
	}

	// Check if handler exists
	handler, ok := r.handlers[name]
	if !ok {
		return nil, fmt.Errorf("no handler for skill: %s", name)
	}

	// Execute handler
	return handler(args)
}

// HasHandler checks if a skill has a registered handler.
func (r *Registry) HasHandler(name string) bool {
	_, ok := r.handlers[name]
	return ok
}

// SaveSkill saves a skill definition to the skills directory.
func (r *Registry) SaveSkill(skill Skill) error {
	// Ensure directory exists
	if err := os.MkdirAll(r.skillsDir, 0755); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(skill, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal skill: %w", err)
	}

	// Write file
	filename := strings.ReplaceAll(skill.Name, " ", "_") + ".json"
	path := filepath.Join(r.skillsDir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write skill file: %w", err)
	}

	// Register the skill
	r.skills[skill.Name] = skill
	return nil
}

// DeleteSkill removes a skill from the registry and deletes its file.
func (r *Registry) DeleteSkill(name string) error {
	// Remove from registry
	delete(r.skills, name)
	delete(r.handlers, name)

	// Delete file if exists
	filename := strings.ReplaceAll(name, " ", "_") + ".json"
	path := filepath.Join(r.skillsDir, filename)
	if _, err := os.Stat(path); err == nil {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to delete skill file: %w", err)
		}
	}

	return nil
}

// Count returns the number of registered skills.
func (r *Registry) Count() int {
	return len(r.skills)
}
