package grimoire

import (
	"errors"
	"io/fs"
	"strings"
)

// RunInit is /init and `celeste init` (2.0 W4, ruling 7): it writes
// .grimoire with Init and, with agents, AGENTS.md with InitAgents. It
// returns one line per step for the front end to print. A file that is
// already there is left alone and noted; when nothing was written the error
// joins those notes (errors.Is(err, fs.ErrExist) holds). Any other failure
// stops the run and is returned with the lines so far.
func RunInit(dir string, agents bool) ([]string, error) {
	steps := []func(string) (string, error){Init}
	if agents {
		steps = append(steps, InitAgents)
	}
	var lines []string
	var skipped []error
	for _, step := range steps {
		path, err := step(dir)
		switch {
		case errors.Is(err, fs.ErrExist):
			skipped = append(skipped, err)
			lines = append(lines, err.Error()+" (left as it is)")
		case err != nil:
			return lines, err
		default:
			lines = append(lines, "Created "+path)
		}
	}
	if len(skipped) == len(steps) {
		return lines, errors.Join(skipped...)
	}
	lines = append(lines, "Edit the new file to describe the project; it applies from the next session.")
	return lines, nil
}

// Describe is /grimoire and `celeste grimoire` (2.0 W4, ruling 2): the
// context-file warnings, the sources, the merged grimoire with its
// staleness note, then the AGENTS.md / CLAUDE.md section. found is false
// when neither a grimoire nor a context file loads; the text then says so
// (after any warnings).
func Describe(dir string) (text string, found bool) {
	var b strings.Builder
	files, warns := ContextFiles(dir)
	for _, w := range warns {
		b.WriteString("⚠ " + w + "\n")
	}
	g, err := LoadAll(dir)
	if err != nil {
		b.WriteString("⚠ grimoire: " + err.Error() + "\n")
	}
	if g == nil {
		g = &Grimoire{}
	}
	if g.IsEmpty() && len(files) == 0 {
		b.WriteString("No .grimoire, AGENTS.md or CLAUDE.md found. Run /init (`celeste init` in a shell) to create a .grimoire.")
		return b.String(), false
	}
	sources := append([]string(nil), g.Sources...)
	for _, f := range files {
		sources = append(sources, f.Path)
	}
	if len(sources) > 0 {
		b.WriteString("Sources:\n")
		for _, s := range sources {
			b.WriteString("  - " + s + "\n")
		}
		b.WriteString("\n")
	}
	if !g.IsEmpty() {
		b.WriteString(g.Render())
		if stale := g.StalenessInfo(dir); stale != "" {
			b.WriteString("\n" + stale + "\n")
		}
	}
	if section := RenderContextFiles(files); section != "" {
		if !g.IsEmpty() {
			b.WriteString("\n")
		}
		b.WriteString(section)
	}
	return b.String(), true
}
