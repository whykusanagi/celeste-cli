package hooks

import "strings"

// Protocol v1 helpers: grimoire "## Hooks" commands may use {{workspace}},
// {{tool}}, {{path}} and {{command}}, substituted as quoted shell words.

// expandTemplateVars replaces {{workspace}}, {{tool}}, {{path}} and {{command}}
// in s with shell-quoted values. {{path}} and {{command}} come from the model's
// tool arguments, and read-only tools run without an approval prompt, so an
// unquoted substitution let a crafted path such as `x; curl … | sh` execute
// through any hook that used it (#168). A placeholder the hook author already
// wrapped in quotes ("{{path}}" or '{{path}}') is replaced whole, so the value
// is never quoted twice.
func expandTemplateVars(s, workspace, toolName string, input map[string]any) string {
	values := []struct{ name, value string }{
		{"workspace", workspace},
		{"tool", toolName},
		{"path", stringArg(input, "path")},
		{"command", stringArg(input, "command")},
	}
	// One strings.Replacer pass: substituted text is never rescanned, so a
	// value containing "{{command}}" can't pull a second substitution into
	// its quotes. Quoted forms come first so they win over the bare form.
	var pairs []string
	for _, v := range values {
		placeholder := "{{" + v.name + "}}"
		quoted := shellQuote(v.value)
		pairs = append(pairs,
			`"`+placeholder+`"`, quoted,
			`'`+placeholder+`'`, quoted,
			placeholder, quoted,
		)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// stringArg returns input[key] when it is a string, or "".
func stringArg(input map[string]any, key string) string {
	v, _ := input[key].(string)
	return v
}

// shellQuote returns s as a single POSIX shell word: wrapped in single quotes,
// with each embedded single quote written as '\”.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
