// Package personacrypttest seals a synthetic persona under a public test
// key, so tests exercise decryption and realistic profile sizes without the
// real key or corpus (W5 ruling 19). The synthetic text is filler, not
// Celeste's persona.
package personacrypttest

import (
	"encoding/json"
	"strings"
	"testing/fstest"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts/personacrypt"
)

// Key is the public test key. It is not, and must never become, the real
// persona key.
const Key = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

// The pins the synthetic persona claims.
const (
	CoreCommit      = "1111111111111111111111111111111111111111"
	ContainerCommit = "2222222222222222222222222222222222222222"
)

// sizes approximate the real builds in bytes: full ~10.5k tokens, spine
// ~6k, lite ~3k. off is the voice boundary rule alone, as in the real build.
var sizes = map[string]int{"full": 42000, "spine": 24000, "lite": 12000}

// Plaintexts are synthetic container builds (celeste_<profile>.json
// bytes), each ending with voiceBoundary as the container's builds do.
func Plaintexts(voiceBoundary string) map[string][]byte {
	out := make(map[string][]byte, len(personacrypt.Profiles))
	for _, p := range personacrypt.Profiles {
		prompt, files := voiceBoundary, []string{}
		if n, ok := sizes[p]; ok {
			head := "Synthetic " + p + " test persona. I never say a file was written unless a tool actually returned that result this turn.\n\n"
			filler := strings.Repeat("filler ", (n-len(head)-len(voiceBoundary))/len("filler ")+1)
			prompt = head + filler + "\n\n" + voiceBoundary
			files = []string{"collections/synthetic_" + p + ".md"}
		}
		body, err := json.Marshal(map[string]any{
			"profile": p, "character": "Celeste", "source_commit": CoreCommit, "files": files,
			"bytes": len(prompt), "approx_tokens": len(prompt) / 4, "system_prompt": prompt,
		})
		if err != nil {
			panic(err)
		}
		out[p] = body
	}
	return out
}

// FS is persona/ as Task 3's sync writes it, sealed under Key: SOURCE.json
// and celeste_<profile>.enc at the root.
func FS(voiceBoundary string) fstest.MapFS {
	key, err := personacrypt.ParseKey(Key)
	if err != nil {
		panic(err)
	}
	src, files, err := personacrypt.SealSet(key, CoreCommit, ContainerCommit, Plaintexts(voiceBoundary), nil, nil)
	if err != nil {
		panic(err)
	}
	m := fstest.MapFS{"SOURCE.json": {Data: src.JSON()}}
	for name, data := range files {
		m[name] = &fstest.MapFile{Data: data}
	}
	return m
}
