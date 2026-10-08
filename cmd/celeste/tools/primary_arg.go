package tools

import "encoding/json"

// PrimaryArger is implemented by a tool that names its primary argument
// itself; PrimaryArg otherwise reads it from the tool's schema.
type PrimaryArger interface {
	PrimaryArg() string
}

// primaryArgKeys are the fields a tool's primary argument is looked for
// in: the order permission rules have always checked them, then url and
// query.
var primaryArgKeys = []string{"command", "path", "content", "pattern", "url", "query"}

// PrimaryArg is the input field an argument-scoped permission rule
// ("bash(git *)", "write_file(src/*)") is matched against, and the one a
// permission prompt leads with: the tool's own choice (PrimaryArger), else
// the first of primaryArgKeys its schema declares, else its only declared
// string property. "" means none: an argument-scoped rule then permits
// nothing for the tool and still restricts it. Only declared fields count, so a field the model adds
// that the tool never reads cannot stand in for the real argument.
func PrimaryArg(t Tool) string {
	if pa, ok := t.(PrimaryArger); ok {
		return pa.PrimaryArg()
	}
	var schema struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
	}
	if json.Unmarshal(t.Parameters(), &schema) != nil {
		return ""
	}
	for _, k := range primaryArgKeys {
		if _, ok := schema.Properties[k]; ok {
			return k
		}
	}
	only := ""
	for k, p := range schema.Properties {
		if p.Type == "string" {
			if only != "" {
				return ""
			}
			only = k
		}
	}
	return only
}
