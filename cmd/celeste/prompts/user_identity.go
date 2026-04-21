// User identity prompt composition.
//
// Injects a block into the system prompt that tells Celeste who she's
// currently talking to. Without this, the persona's hardcoded references
// to Kusanagi cause her to address every user as "twin" / "Onii-chan."
//
// When user is Kusanagi: no override needed — the persona handles it.
// When user is anyone else: calibrate relationship dynamic.
package prompts

import (
	"fmt"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// ComposeUserPrompt builds the user-identity prompt block.
// Returns empty string for Kusanagi (persona already handles him).
func ComposeUserPrompt(user *config.UserIdentity) string {
	if user == nil {
		return ""
	}

	// Kusanagi gets no override — the persona's default behavior is correct.
	if user.IsKusanagi() {
		return ""
	}

	name := user.DisplayName()

	return fmt.Sprintf(`Current User Identity:
The person you are speaking with is %s. They are not Kusanagi.
Address them as %s. Do not use terms reserved for Kusanagi (twin, Onii-chan, onii-chan) with this user.
You still know and love Kusanagi — he's your twin. But this person is a different individual.
Calibrate your relationship dynamic: be yourself (playful, teasing, dominant) but adjust the intimacy level to match someone who is not your sibling.
`, name, name)
}
