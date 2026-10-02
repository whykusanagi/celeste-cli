// Package shellparse reads a shell command line roughly as a shell would:
// quotes (including $'...' escapes and $"..."), a leading backslash,
// unquoted $IFS as a word break, operators, subshells, $( ) and backtick
// substitutions, and the strings run by sh -c, eval and ssh. It does not
// expand other variables, globs or braces. It has no dependencies. The
// blocking bash check (tools/builtin) and the advisory destructive-bash
// rule and watchdog (rules) both read commands with it, and share its rm
// policy (DestructiveRm), so the rule sees whatever the check refuses.
package shellparse

import (
	"path"
	"strings"
)

// MaxDepth bounds recursion into nested shells (sh -c, eval, ssh,
// substitutions).
const MaxDepth = 4

// Shells are the commands whose -c argument is itself a command line.
var Shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true}

// Wrappers run their arguments as a command.
var Wrappers = map[string]bool{"sudo": true, "command": true, "exec": true, "nohup": true, "env": true, "time": true, "nice": true, "doas": true, "builtin": true}

// CommandName is a command word as the shell resolves it: a leading
// backslash removed and the path dropped (/bin/rm -> rm). Quotes are
// already gone (Segments removes them).
func CommandName(w string) string {
	w = strings.TrimLeft(w, `\`)
	return path.Base(w)
}

// Result is what Walk found.
type Result int

const (
	// None: fn returned false for every command.
	None Result = iota
	// Found: fn returned true for some command.
	Found
	// TooDeep: the line nests command strings past MaxDepth, so not every
	// command was visited. A caller that blocks should refuse.
	TooDeep
)

// Walk calls fn with every simple command in cmd (a word list, quotes
// removed), including the commands nested in $( ), backticks, the command
// string of sh/bash/... -c (wherever -c sits among the shell's options),
// script -c, env -S, eval arguments and an ssh remote command, each also as
// its own command line. It stops at the first command fn returns true for.
func Walk(cmd string, fn func(words []string) bool) Result {
	return walk(cmd, 0, fn)
}

func walk(cmd string, depth int, fn func(words []string) bool) Result {
	if depth > MaxDepth {
		return TooDeep
	}
	segs, nested := Segments(cmd)
	for _, n := range nested {
		if r := walk(n, depth+1, fn); r != None {
			return r
		}
	}
	for _, words := range segs {
		if fn(words) {
			return Found
		}
		// Command strings anywhere in the segment (docker exec web sh -c,
		// sudo bash -lc, script -qc, env -S).
		for i := range words {
			for _, inner := range commandStrings(CommandName(words[i]), words[i+1:]) {
				if r := walk(inner, depth+1, fn); r != None {
					return r
				}
			}
		}
		name, args := Command(words)
		switch name {
		case "eval":
			if r := walk(strings.Join(args, " "), depth+1, fn); r != None {
				return r
			}
		case "ssh":
			// ssh [opts] host command...: the remote command.
			for k := 0; k < len(args); k++ {
				if strings.HasPrefix(args[k], "-") {
					if len(args[k]) == 2 && strings.ContainsAny(args[k][1:], "bcDEeFIiJLlmOopQRSWw") {
						k++ // an option that takes a value
					}
					continue
				}
				if r := walk(strings.Join(args[k+1:], " "), depth+1, fn); r != None {
					return r
				}
				break
			}
		}
	}
	return None
}

// shellValueOptions take the next word as their value.
var shellValueOptions = map[string]bool{"-o": true, "+o": true, "-O": true, "+O": true, "--rcfile": true, "--init-file": true}

// commandStrings returns the command lines that name, run with args, would
// execute itself: a shell's -c string, script's -c/--command, env's
// -S/--split-string. Anything else returns nil.
func commandStrings(name string, args []string) []string {
	switch {
	case Shells[name]:
		// sh [options] -c [--] 'string': -c may be alone or in a cluster
		// (-lc, -ec, -xc) and other options may come first.
		c := false
		k := 0
		for ; k < len(args); k++ {
			a := args[k]
			if a == "--" {
				k++
				break
			}
			if !strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "+") || a == "-" {
				break
			}
			if shellValueOptions[a] {
				k++
				continue
			}
			if !strings.HasPrefix(a, "--") && strings.Contains(a[1:], "c") {
				c = true
			}
		}
		if c && k < len(args) {
			return []string{args[k]}
		}
	case name == "script":
		for k := 0; k < len(args); k++ {
			a := args[k]
			switch {
			case a == "--command" && k+1 < len(args):
				return []string{args[k+1]}
			case strings.HasPrefix(a, "--command="):
				return []string{strings.TrimPrefix(a, "--command=")}
			case strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--"):
				if i := strings.IndexByte(a, 'c'); i > 0 {
					if rest := a[i+1:]; rest != "" {
						return []string{rest} // -c'cmd' attached
					}
					if k+1 < len(args) {
						return []string{args[k+1]}
					}
				}
			}
		}
	case name == "env":
		for k := 0; k < len(args); k++ {
			a := args[k]
			switch {
			case (a == "-S" || a == "--split-string") && k+1 < len(args):
				return []string{strings.Join(args[k+1:], " ")}
			case strings.HasPrefix(a, "--split-string="):
				return []string{strings.Join(append([]string{strings.TrimPrefix(a, "--split-string=")}, args[k+1:]...), " ")}
			case strings.HasPrefix(a, "-S"):
				return []string{strings.Join(append([]string{a[2:]}, args[k+1:]...), " ")}
			case strings.HasPrefix(a, "-") || strings.Contains(a, "="):
				continue
			}
			return nil
		}
	}
	return nil
}

// Command skips assignments (FOO=1, also after env) and wrappers (sudo, env,
// nohup, ... and their flags) to the command word, and returns its name
// (CommandName) and arguments. name is "" when there is no command word.
func Command(words []string) (name string, args []string) {
	i := 0
	for i < len(words) {
		w := CommandName(words[i])
		if strings.Contains(words[i], "=") && !strings.HasPrefix(words[i], "-") {
			i++
			continue
		}
		if Wrappers[w] {
			i++
			for i < len(words) && strings.HasPrefix(words[i], "-") {
				i++
			}
			continue
		}
		break
	}
	if i >= len(words) {
		return "", nil
	}
	return CommandName(words[i]), words[i+1:]
}

// Segments splits a command line into simple commands (word lists,
// quotes removed) at ; & && || | newlines and parentheses, and returns the
// bodies of $( ) and backtick substitutions, wherever they appear (unquoted
// or in double quotes), as nested commands.
func Segments(s string) (segs [][]string, nested []string) {
	// A backslash-newline is a line continuation: the shell drops both.
	s = strings.ReplaceAll(s, "\\\n", "")
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
			case '\\':
				j++ // an escaped character is never a paren
			case '\'':
				// A quoted ) does not close the substitution.
				if end := strings.IndexByte(s[j+1:], '\''); end >= 0 {
					j += end + 1
				} else {
					j = len(s)
				}
			case '"':
				for j++; j < len(s) && s[j] != '"'; j++ {
					if s[j] == '\\' {
						j++
					}
				}
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
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			// $'...': ANSI-C quoting, backslash escapes decoded.
			inWord = true
			i = ansiCQuote(s, i+2, &cur)
		case c == '$' && i+1 < len(s) && s[i+1] == '"':
			// $"...": a locale-translated string, otherwise "...".
			i++
		case c == '$' && ifsAt(s, i) > 0:
			// Unquoted $IFS / ${IFS} splits words like a space does.
			endWord()
			i += ifsAt(s, i)
		case c == '#' && !inWord:
			// A comment runs to the end of the line.
			if end := strings.IndexByte(s[i:], '\n'); end >= 0 {
				i += end
			} else {
				i = len(s)
			}
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

// ifsAt returns the length of an unquoted $IFS or ${IFS} at s[i] ("$IFS"
// not followed by a name character), or 0.
func ifsAt(s string, i int) int {
	if strings.HasPrefix(s[i:], "${IFS}") {
		return len("${IFS}")
	}
	if strings.HasPrefix(s[i:], "$IFS") {
		j := i + len("$IFS")
		if j < len(s) && (s[j] == '_' || s[j] >= '0' && s[j] <= '9' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z') {
			return 0
		}
		return len("$IFS")
	}
	return 0
}

// ansiCQuote decodes the body of a $'...' string starting at s[i] into cur
// and returns the index after the closing quote. It handles the escapes
// that can spell a path or a command name: \\ \' \" \n \t \xHH and
// \NNN (octal); any other escaped character stands for itself.
func ansiCQuote(s string, i int, cur *strings.Builder) int {
	for i < len(s) {
		c := s[i]
		if c == '\'' {
			return i + 1
		}
		if c != '\\' || i+1 >= len(s) {
			cur.WriteByte(c)
			i++
			continue
		}
		e := s[i+1]
		i += 2
		switch {
		case e == 'n':
			cur.WriteByte('\n')
		case e == 't':
			cur.WriteByte('\t')
		case e == 'x':
			v, n := 0, 0
			for n < 2 && i < len(s) && isHex(s[i]) {
				v = v*16 + hexVal(s[i])
				i++
				n++
			}
			if n == 0 {
				cur.WriteString(`\x`)
			} else {
				cur.WriteByte(byte(v))
			}
		case e >= '0' && e <= '7':
			v, n := int(e-'0'), 1
			for n < 3 && i < len(s) && s[i] >= '0' && s[i] <= '7' {
				v = v*8 + int(s[i]-'0')
				i++
				n++
			}
			cur.WriteByte(byte(v))
		default:
			cur.WriteByte(e)
		}
	}
	return i
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}
