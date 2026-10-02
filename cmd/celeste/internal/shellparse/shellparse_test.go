package shellparse

import (
	"reflect"
	"strings"
	"testing"
)

func TestSegmentsQuotesOperatorsAndSubstitutions(t *testing.T) {
	segs, nested := Segments(`'rm' -rf "a b" && echo $(ls -l) | wc; x` + "`id`")
	want := [][]string{{"rm", "-rf", "a b"}, {"echo"}, {"wc"}, {"x"}}
	if !reflect.DeepEqual(segs, want) {
		t.Errorf("segs = %q, want %q", segs, want)
	}
	if !reflect.DeepEqual(nested, []string{"ls -l", "id"}) {
		t.Errorf("nested = %q", nested)
	}
}

func TestCommandNameAndCommand(t *testing.T) {
	for in, want := range map[string]string{`\rm`: "rm", "/bin/rm": "rm", "rm": "rm", "./x": "x"} {
		if got := CommandName(in); got != want {
			t.Errorf("CommandName(%q) = %q, want %q", in, got, want)
		}
	}
	name, args := Command([]string{"FOO=1", "env", "-i", "BAR=2", "nohup", "/bin/rm", "-rf", "x"})
	if name != "rm" || !reflect.DeepEqual(args, []string{"-rf", "x"}) {
		t.Errorf("Command = %q %q", name, args)
	}
	if name, _ := Command([]string{"FOO=1"}); name != "" {
		t.Errorf("assignment only: name = %q", name)
	}
}

func TestWalkVisitsNestedShells(t *testing.T) {
	var seen []string
	Walk(`bash -c 'eval "ssh -p 22 host touch remote"'; echo $(sh -c "touch sub")`, func(w []string) bool {
		seen = append(seen, strings.Join(w, " "))
		return false
	})
	for _, want := range []string{"touch remote", "touch sub"} {
		found := false
		for _, s := range seen {
			if s == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Walk never visited %q; saw %q", want, seen)
		}
	}
}

func TestWalkStopsAtMaxDepth(t *testing.T) {
	cmd := "true"
	for i := 0; i <= MaxDepth+1; i++ {
		cmd = "eval " + cmd
	}
	if Walk(cmd, func([]string) bool { return false }) != TooDeep {
		t.Error("nesting past MaxDepth must report true")
	}
	if Walk("eval eval true", func([]string) bool { return false }) != None {
		t.Error("shallow nesting reported true")
	}
}

func TestSegmentsIFSAndDollarQuotes(t *testing.T) {
	for in, want := range map[string][]string{
		`rm -rf${IFS}/`:          {"rm", "-rf", "/"},
		`rm$IFS-rf$IFS/x`:        {"rm", "-rf", "/x"},
		`echo $IFSX`:             {"echo", "$IFSX"},
		`rm -rf $'\x2fusr'`:      {"rm", "-rf", "/usr"},
		`rm -rf $'\057' $'a\'b'`: {"rm", "-rf", "/", "a'b"},
		`$'rm' $"-rf" x`:         {"rm", "-rf", "x"},
		`echo "${IFS}"`:          {"echo", "${IFS}"},
	} {
		segs, _ := Segments(in)
		if len(segs) != 1 || !reflect.DeepEqual(segs[0], want) {
			t.Errorf("Segments(%q) = %q, want [%q]", in, segs, want)
		}
	}
}

func TestSegmentsContinuationCommentsAndQuotedParens(t *testing.T) {
	for in, want := range map[string][][]string{
		"rm -r -f \\\n/usr": {{"rm", "-r", "-f", "/usr"}},
		"r\\\nm x":          {{"rm", "x"}},
		"echo # '\nrm x":    {{"echo"}, {"rm", "x"}},
		"echo a#b":          {{"echo", "a#b"}},
		"ls # rm -r -f /":   {{"ls"}},
	} {
		if segs, _ := Segments(in); !reflect.DeepEqual(segs, want) {
			t.Errorf("Segments(%q) = %q, want %q", in, segs, want)
		}
	}
	for in, want := range map[string]string{
		`echo $(echo ")"; rm x)`: `echo ")"; rm x`,
		`echo $(echo ')'; rm x)`: `echo ')'; rm x`,
		`echo $(echo \); rm x)`:  `echo \); rm x`,
	} {
		if _, nested := Segments(in); len(nested) != 1 || nested[0] != want {
			t.Errorf("Segments(%q) nested = %q, want [%q]", in, nested, want)
		}
	}
}

func TestCommandStrings(t *testing.T) {
	for _, tc := range []struct {
		words []string
		want  []string
	}{
		{[]string{"bash", "-lc", "X"}, []string{"X"}},
		{[]string{"sh", "-xc", "X"}, []string{"X"}},
		{[]string{"bash", "--norc", "-c", "X"}, []string{"X"}},
		{[]string{"bash", "-c", "--", "X"}, []string{"X"}},
		{[]string{"bash", "-o", "pipefail", "-c", "X"}, []string{"X"}},
		{[]string{"bash", "script.sh"}, nil},
		{[]string{"bash", "-l", "script.sh"}, nil},
		{[]string{"script", "-qc", "X", "/dev/null"}, []string{"X"}},
		{[]string{"script", "--command", "X"}, []string{"X"}},
		{[]string{"script", "--command=X"}, []string{"X"}},
		{[]string{"env", "-S", "rm x"}, []string{"rm x"}},
		{[]string{"env", "-Srm x"}, []string{"rm x"}},
		{[]string{"env", "--split-string=rm", "x"}, []string{"rm x"}},
		{[]string{"env", "FOO=1", "ls"}, nil},
	} {
		got := commandStrings(CommandName(tc.words[0]), tc.words[1:])
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("commandStrings(%q) = %q, want %q", tc.words, got, tc.want)
		}
	}
}

// FuzzWalk: any input terminates without panicking and visits only
// non-empty word lists.
func FuzzWalk(f *testing.F) {
	for _, seed := range []string{
		`rm -rf /`, `bash -lc "rm -rf ~"`, `echo $(echo ")"; rm -r -f /usr)`,
		"echo # '\nrm -r -f /usr", "rm -rf${IFS}/", `rm -rf $'\x2f'`, `env -S'rm -r -f /'`,
		`script -qc 'eval "sh -c \"rm -r /\""'`, "`", "$(", `$'`, `"\`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		Walk(s, func(words []string) bool {
			if len(words) == 0 {
				t.Fatalf("empty command from %q", s)
			}
			return false
		})
	})
}
