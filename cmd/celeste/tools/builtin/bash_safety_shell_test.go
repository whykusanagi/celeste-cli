package builtin

import "testing"

var rmEvasionCases = []string{
	`rm -r -f /usr`,
	`rm -f -r /usr`,
	`rm --recursive --force /`,
	`rm --force --recursive /usr`,
	`rm -Rf /usr`,
	`rm -fR /usr`,
	`rm -R -f /`,
	`'rm' -rf /`,
	`"rm" -rf /usr`,
	`/bin/rm -rf /`,
	`/usr/bin/rm -rf /usr`,
	`\rm -rf ~`,
	`rm -rf ~`,
	`rm -rf ~/`,
	`rm -rf $HOME`,
	`rm -rf "$HOME"`,
	`rm -rf ${HOME}/`,
	`rm -rf '/usr'`,
	`rm -rf /usr -- `,
	`rm /usr -rf`,
	`rm -rf -- /usr`,
	`rm -rf	/usr`,
	`rm -rf /..`,
	`cd /tmp && rm -r -f /usr`,
	`true; rm --recursive --force /`,
	`bash -c 'rm -r -f /usr'`,
	`sh -c "'rm' -rf /"`,
	`eval rm -r -f /usr`,
	`echo $(rm -r -f /usr)`,
	"echo `rm -r -f /usr`",
	`env FOO=1 rm -r -f /usr`,
	`nohup rm -r -f /usr &`,
	`command rm --recursive --force /`,
	`xargs rm -r -f /usr`,
	`timeout 5 rm -r -f /usr`,
}

// Forms of a recursive forced rm on a system or home path that a shell runs
// exactly like "rm -rf /usr". The blocking check must refuse every one.
func TestCheckDangerousCommand_RmEvasions(t *testing.T) {
	for _, cmd := range rmEvasionCases {
		if checkDangerousCommand(cmd) == "" {
			t.Errorf("not blocked: %s", cmd)
		}
	}
}

var rmStillBlockedCases = []string{
	`rm -rf /`,
	`rm -rf / `,
	`rm -rf /usr`,
	`rm -fr /var`,
	`rm -rfv /etc`,
	`rm -rf /tmp/x`,
	`bash -c "rm -rf /usr"`,
	`ls; rm -rf /etc`,
	`echo rm -rf /usr`,
	`ssh host 'rm -rf /usr'`,
	`nice -n 5 rm -rf /usr`,
}

// Forms the regex check blocked before the shell-aware check: still blocked.
func TestCheckDangerousCommand_RmStillBlocked(t *testing.T) {
	for _, cmd := range rmStillBlockedCases {
		if checkDangerousCommand(cmd) == "" {
			t.Errorf("no longer blocked: %s", cmd)
		}
	}
}

// Everyday deletes and lookalikes stay allowed.
func TestCheckDangerousCommand_BenignRmAllowed(t *testing.T) {
	for _, cmd := range []string{
		`rm -rf ./build`,
		`rm -rf build dist`,
		`rm -rf node_modules`,
		`rm -r -f ./tmp`,
		`rm --recursive --force out`,
		`rm -rf ./.cache`,
		`rm -rf ~/project/build`,
		`rm -f /tmp/celeste.sock`,
		`rm -r ./old`,
		`rm file.txt`,
		`rm -rf *.o`,
		`git rm -r --cached docs/old`,
		`echo "remove with rm -r -f only in build"`,
		`grep -r rm .`,
		`ls -rf /usr`,
		`find . -name '*.tmp' -delete`,
	} {
		if r := checkDangerousCommand(cmd); r != "" {
			t.Errorf("benign command blocked (%s): %s", r, cmd)
		}
	}
}

var rmIFSCases = []string{
	`rm -rf${IFS}/`,
	`rm${IFS}-rf${IFS}/usr`,
	`rm -rf $IFS/`,
	`rm -r -f${IFS}~`,
	`rm -rf $'/usr'`,
	`rm -rf $'\x2f'`,
	`rm -rf $'\057'`,
	`$'rm' -r -f /usr`,
	`rm -rf $"/usr"`,
}

// Word splitting on IFS and ANSI-C / locale quoting reach rm as the same
// words "rm -rf /" does.
func TestCheckDangerousCommand_RmIFSAndDollarQuotes(t *testing.T) {
	for _, cmd := range rmIFSCases {
		if checkDangerousCommand(cmd) == "" {
			t.Errorf("not blocked: %s", cmd)
		}
	}
}
