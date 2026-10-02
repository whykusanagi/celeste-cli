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
	if !Walk(cmd, func([]string) bool { return false }) {
		t.Error("nesting past MaxDepth must report true")
	}
	if Walk("eval eval true", func([]string) bool { return false }) {
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
