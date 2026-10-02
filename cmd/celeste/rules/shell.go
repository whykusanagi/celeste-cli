package rules

import (
	"path"
	"strings"
)

// buildDirs are build output a project regenerates: removing one inside
// the workspace is routine, not destructive.
var buildDirs = map[string]bool{"build": true, "dist": true, "node_modules": true, "target": true, ".cache": true, "out": true, "coverage": true}

// maxShellDepth bounds recursion into nested shells (sh -c, eval, ssh).
const maxShellDepth = 4

// destructiveBash fires on a force push, or on an rm that is both
// recursive and forced (-rf, -r -f, --recursive --force, flags before or
// after the paths) unless every path it removes is a build directory (or
// inside one) under the workspace. It reads the command as a shell would,
// roughly: quotes, operators, subshells ( ), $( ) and backticks, and the
// string run by sh/bash/zsh -c, eval and ssh. Not covered: find -delete,
// xargs rm, scripts the command runs.
func destructiveBash(h Hit) bool {
	if h.Call == nil {
		return true
	}
	cmd, _ := h.Call.Input["command"].(string)
	return destructiveShell(cmd, 0)
}

func destructiveShell(cmd string, depth int) bool {
	if depth > maxShellDepth {
		return true // nested past reason: err on the side of asking
	}
	segs, nested := shellSegments(cmd)
	for _, n := range nested {
		if destructiveShell(n, depth+1) {
			return true
		}
	}
	for _, seg := range segs {
		if destructiveSegment(seg, depth) {
			return true
		}
	}
	return false
}

// shellWrappers run their arguments as a command.
var shellWrappers = map[string]bool{"sudo": true, "command": true, "exec": true, "nohup": true, "env": true, "time": true, "nice": true, "doas": true, "builtin": true}

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true}

func destructiveSegment(words []string, depth int) bool {
	// Any sh -c '...' in the segment (docker exec web sh -c, sudo bash -c).
	for i := 0; i+2 < len(words); i++ {
		if shells[commandName(words[i])] && words[i+1] == "-c" && destructiveShell(words[i+2], depth+1) {
			return true
		}
	}
	// Skip assignments and wrappers to the command word.
	i := 0
	for i < len(words) {
		w := commandName(words[i])
		if strings.Contains(words[i], "=") && !strings.HasPrefix(words[i], "-") && i == 0 {
			i++
			continue
		}
		if shellWrappers[w] {
			i++
			for i < len(words) && strings.HasPrefix(words[i], "-") {
				i++
			}
			continue
		}
		break
	}
	if i >= len(words) {
		return false
	}
	name, args := commandName(words[i]), words[i+1:]
	switch name {
	case "rm":
		return rmRecursiveForce(args)
	case "git":
		return gitForcePush(args)
	case "eval":
		return destructiveShell(strings.Join(args, " "), depth+1)
	case "ssh":
		// ssh [opts] host command...: the remote command.
		for k := 0; k < len(args); k++ {
			if strings.HasPrefix(args[k], "-") {
				if len(args[k]) == 2 && strings.ContainsAny(args[k][1:], "bcDEeFIiJLlmOopQRSWw") {
					k++ // an option that takes a value
				}
				continue
			}
			return destructiveShell(strings.Join(args[k+1:], " "), depth+1)
		}
	}
	return false
}

// commandName is a command word as the shell resolves it: quotes and a
// leading backslash removed, and the path dropped (/bin/rm → rm).
func commandName(w string) string {
	w = strings.TrimLeft(w, `\`)
	return path.Base(w)
}

func gitForcePush(args []string) bool {
	push := false
	for _, a := range args {
		switch {
		case a == "push":
			push = true
		case push && (a == "--force" || a == "-f" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "f"))):
			return true
		}
	}
	return false
}

func rmRecursiveForce(args []string) bool {
	recursive, force := false, false
	var targets []string
	endOfFlags := false
	for k := 0; k < len(args); k++ {
		a := args[k]
		switch {
		case isRedirect(a):
			if redirectTakesNext(a) {
				k++
			}
		case endOfFlags:
			targets = append(targets, a)
		case a == "--":
			endOfFlags = true
		case a == "--recursive":
			recursive = true
		case a == "--force":
			force = true
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			recursive = recursive || strings.ContainsAny(a, "rR")
			force = force || strings.Contains(a, "f")
		default:
			targets = append(targets, a)
		}
	}
	if !recursive || !force {
		return false
	}
	if len(targets) == 0 {
		return true
	}
	for _, t := range targets {
		if !buildDirTarget(t) {
			return true
		}
	}
	return false
}

// isRedirect: >, >>, 2>, &>, 2>&1, >file, <file ...
func isRedirect(w string) bool {
	t := strings.TrimLeft(w, "0123456789&")
	return strings.HasPrefix(t, ">") || strings.HasPrefix(t, "<")
}

// redirectTakesNext: a bare operator ("> file") names its target in the
// next word; "2>&1" and ">file" do not.
func redirectTakesNext(w string) bool {
	t := strings.TrimLeft(w, "0123456789&")
	t = strings.TrimLeft(t, "<>")
	return t == "" || t == "|"
}

// buildDirTarget: a relative path inside the workspace whose first element
// is a build directory.
func buildDirTarget(t string) bool {
	if t == "" || strings.ContainsAny(t, "$~*?`\\") || strings.HasPrefix(t, "/") {
		return false
	}
	clean := path.Clean(t)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	first, _, _ := strings.Cut(clean, "/")
	return buildDirs[first]
}

// shellSegments splits a command line into simple commands (word lists,
// quotes removed) at ; & && || | newlines and parentheses, and returns the
// bodies of $( ) and backtick substitutions, wherever they appear (unquoted
// or in double quotes), as nested commands.
func shellSegments(s string) (segs [][]string, nested []string) {
	var words []string
	var cur strings.Builder
	inWord := false
	endWord := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	endSeg := func() {
		endWord()
		if len(words) > 0 {
			segs = append(segs, words)
		}
		words = nil
	}
	// substitution reads a $( ) or backtick body starting at s[i] (just
	// after the opener) and returns it and the index after the closer.
	substitution := func(i int, backtick bool) (string, int) {
		if backtick {
			end := strings.IndexByte(s[i:], '`')
			if end < 0 {
				return s[i:], len(s)
			}
			return s[i : i+end], i + end + 1
		}
		depth := 1
		for j := i; j < len(s); j++ {
			switch s[j] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return s[i:j], j + 1
				}
			}
		}
		return s[i:], len(s)
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\'':
			inWord = true
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				cur.WriteString(s[i+1:])
				i = len(s)
				continue
			}
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 2
		case c == '"':
			inWord = true
			i++
			for i < len(s) && s[i] != '"' {
				switch {
				case s[i] == '\\' && i+1 < len(s):
					cur.WriteByte(s[i+1])
					i += 2
				case s[i] == '`':
					body, next := substitution(i+1, true)
					nested = append(nested, body)
					i = next
				case s[i] == '$' && i+1 < len(s) && s[i+1] == '(':
					body, next := substitution(i+2, false)
					nested = append(nested, body)
					i = next
				default:
					cur.WriteByte(s[i])
					i++
				}
			}
			i++
		case c == '\\' && i+1 < len(s):
			// \rm only defeats aliases: the word is rm.
			inWord = true
			cur.WriteByte(s[i+1])
			i += 2
		case c == '`':
			body, next := substitution(i+1, true)
			nested = append(nested, body)
			endWord()
			i = next
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			body, next := substitution(i+2, false)
			nested = append(nested, body)
			endWord()
			i = next
		case c == ';' || c == '\n' || c == '(' || c == ')':
			endSeg()
			i++
		case c == '&' || c == '|':
			// && || | & end the command; &> and >& are redirections.
			if c == '&' && i+1 < len(s) && s[i+1] == '>' {
				inWord = true
				cur.WriteByte(c)
				i++
				continue
			}
			if c == '&' && inWord && strings.HasSuffix(cur.String(), ">") {
				cur.WriteByte(c)
				i++
				continue
			}
			endSeg()
			i++
		case c == ' ' || c == '\t':
			endWord()
			i++
		default:
			inWord = true
			cur.WriteByte(c)
			i++
		}
	}
	endSeg()
	return segs, nested
}
