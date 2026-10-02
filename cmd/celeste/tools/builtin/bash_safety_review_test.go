package builtin

import (
	"strings"
	"testing"
)

// I1: a shell's -c can sit in an option cluster or after other options.
func TestCheckDangerousCommand_ShellOptionForms(t *testing.T) {
	for _, cmd := range []string{
		`bash -lc "rm -rf ~"`,
		`bash -ec 'rm -rf $HOME'`,
		`zsh -ic 'rm -rf ~/'`,
		`sh -xc 'rm -r -f /usr'`,
		`bash --norc -c 'rm -r -f /usr'`,
		`bash -c -- 'rm -r -f /usr'`,
		`bash -o pipefail -c 'rm -r -f /usr'`,
		`sudo -u x bash -lc 'rm -r -f /usr'`,
		`script -c "rm -r -f /usr" /dev/null`,
		`script -qc 'rm -r -f /usr' /dev/null`,
		`script --command 'rm -r -f /usr'`,
		`env -S 'rm -r -f /usr'`,
		`env -S'rm -r -f /usr'`,
		`env --split-string='rm -r -f /usr'`,
	} {
		if checkDangerousCommand(cmd) == "" {
			t.Errorf("not blocked: %s", cmd)
		}
	}
}

// I2: input where a naive parse and the shell disagree about where the
// tail of the line is.
func TestCheckDangerousCommand_ParserDisagreements(t *testing.T) {
	for _, cmd := range []string{
		"rm -r -f \\\n/usr",
		"r\\\nm -r -f /usr",
		"echo # '\nrm -r -f /usr",
		`echo $(echo ")"; rm -r -f /usr)`,
		`echo $(echo ')'; rm -r -f /usr)`,
		`echo $(echo \); rm -r -f /usr)`,
	} {
		if checkDangerousCommand(cmd) == "" {
			t.Errorf("not blocked: %q", cmd)
		}
	}
	// A # inside a word is not a comment, and a real comment hides nothing
	// that runs.
	for _, cmd := range []string{
		`echo a#b`,
		`ls # rm -r -f /usr`,
	} {
		if r := checkDangerousCommand(cmd); r != "" {
			t.Errorf("benign blocked (%s): %q", r, cmd)
		}
	}
}

// I3: with no TTY rm never prompts, so -r alone removes as much as -rf on
// the paths that matter most; GNU long-option prefixes count.
func TestCheckDangerousCommand_RecursiveWithoutForce(t *testing.T) {
	for _, cmd := range []string{
		`rm -r ~`,
		`rm -R $HOME`,
		`rm -r /usr`,
		`rm -r /`,
		`rm --recursive ~/`,
		`rm -r ~root`,
		`rm -r /*`,
		`rm --rec --for /opt/x`,
		`rm --recur --forc /opt/x`,
		`rm --r --f /opt/x`,
		`rm --recursive /etc`,
	} {
		if checkDangerousCommand(cmd) == "" {
			t.Errorf("not blocked: %s", cmd)
		}
	}
	for _, cmd := range []string{
		`rm -r ./build`,
		`rm -r node_modules`,
		`rm -r /opt/app/cache`, // deep path without -f: still needs force
		`rm --recursive dist`,
		`rm -r ~/project/build`,
	} {
		if r := checkDangerousCommand(cmd); r != "" {
			t.Errorf("benign blocked (%s): %s", r, cmd)
		}
	}
}

// M1: nesting past the walker's depth has its own reason.
func TestCheckDangerousCommand_TooDeepReason(t *testing.T) {
	cmd := "true"
	for i := 0; i < 8; i++ {
		cmd = "eval " + cmd
	}
	r := checkDangerousCommand(cmd)
	if !strings.Contains(r, "nesting too deep") {
		t.Fatalf("reason = %q, want the nesting reason", r)
	}
}
