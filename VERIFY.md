# Verifying a Celeste CLI release

Every release ships a **PGP-signed `checksums.txt`** and a signed `manifest.json`.
Verifying them proves the download came from the maintainer and was not tampered with.

Download releases only from the official page:
**https://github.com/whykusanagi/celeste-cli/releases**

Signing key (the primary fingerprint is the trust anchor):

```
whykusanagi <me@whykusanagi.xyz>
9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55
```

The spaced form above and the compact `940490EF09DA31322BF7FD83875849AB1D541C55`
are the same value; `gpg` accepts the compact form.

## Verify the download

From the same release, download into one folder: the binary archive
(e.g. `celeste-darwin-arm64.tar.gz`), `checksums.txt`, `checksums.txt.asc`,
`manifest.json`, and `manifest.json.asc`. Then run:

```bash
# 1. Import the signing key. The repo ships the complete key, including the
#    signing subkey the release was signed with:
curl -fsSL https://raw.githubusercontent.com/whykusanagi/celeste-cli/main/whykusanagi.asc -o whykusanagi.asc
gpg --import whykusanagi.asc

# 2. Confirm the imported primary fingerprint matches the expected value:
gpg --fingerprint 940490EF09DA31322BF7FD83875849AB1D541C55
#   expect: 9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55

# 3. Cross-check that SAME fingerprint against an independent channel, so you are
#    not trusting only the repo copy. This prints GitHub's copy of the key; its
#    primary fingerprint must equal the value in step 2:
curl -fsSL https://github.com/whykusanagi.gpg | gpg --show-keys --with-fingerprint
#   (a third source, if the key is published there: gpg --keyserver keys.openpgp.org \
#    --recv-keys 940490EF09DA31322BF7FD83875849AB1D541C55)

# 4. Verify the signatures (authenticity):
gpg --verify checksums.txt.asc checksums.txt
gpg --verify manifest.json.asc manifest.json

# 5. Verify the archive matches its checksum (integrity):
shasum -a 256 --ignore-missing -c checksums.txt
```

### Reading the results

Success looks like `Good signature from "whykusanagi <me@whykusanagi.xyz>"` in
step 4 and a line ending `<your-archive>: OK` in step 5. Both must pass. Confirm
your actual archive filename appears in the step-5 output: `--ignore-missing`
skips names not listed in `checksums.txt`, so a renamed or wrong file can exit
cleanly without ever being checked.

`checksums.txt` is what gates your archive's integrity. `manifest.json` is the
signed, machine-readable build record (version, commit, per-file checksums, URLs);
verifying its signature confirms that record is authentic, useful for provenance
but not required to trust your download.

`gpg` also prints `WARNING: This key is not certified with a trusted signature`.
That warning is expected and not a failure: it only means you have not marked the
key as trusted in your personal web of trust. The fingerprint match in steps 2 and
3 is what establishes trust here.

Stop and do not run the binary if you see `BAD signature`, `FAILED`, `No public
key`, or a fingerprint in step 3 that differs from step 2. Re-download from the
official release page and verify again; a mismatch means the files or the key do
not belong together.

Releases are signed by a signing subkey certified under the primary key, so
`gpg --verify` reports the subkey id while trust flows from the primary
fingerprint you confirmed.

## Launching on macOS

The macOS binary is ad-hoc signed (not yet notarized through Apple), so macOS
quarantines it on first download. After verifying, extract and clear the flag:

```bash
tar xzf celeste-darwin-arm64.tar.gz   # unpacks the `celeste` binary
xattr -dr com.apple.quarantine ./celeste
```

You do this once. A future release may be Apple-notarized, which removes the step.

Linux and other platforms extract the same way (`tar xzf <archive>`); the
`xattr` quarantine step is macOS-only.
