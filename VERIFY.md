# Verifying a Celeste CLI release

Each release ships with a **PGP-signed `checksums.txt`** (and a signed
`manifest.json`). That signature is the authenticity guarantee: it proves the
download came from the maintainer and wasn't tampered with.

Signing key:

```
whykusanagi <me@whykusanagi.xyz>
fingerprint  9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55
```

## Verify the download

Download the binary archive (e.g. `celeste-darwin-arm64.tar.gz`), `checksums.txt`,
and `checksums.txt.asc` from the same Release into one folder, then:

```bash
# 1. Import the signing key. The repo ships it as whykusanagi.asc and it carries
#    the signing subkey the release was signed with. If you haven't cloned the
#    repo, fetch just the key first:
curl -O https://raw.githubusercontent.com/whykusanagi/celeste-cli/main/whykusanagi.asc
gpg --import whykusanagi.asc

# 2. Confirm the fingerprint, and cross-check it against an independent source:
#    the same primary fingerprint is published at https://github.com/whykusanagi.gpg
gpg --fingerprint 940490EF09DA31322BF7FD83875849AB1D541C55
#   → 9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55

# 3. Verify the signature on the checksum file (authenticity).
gpg --verify checksums.txt.asc checksums.txt        # "Good signature"

# 4. Verify the download matches the checksum (integrity).
shasum -a 256 -c checksums.txt                       # "OK"
```

A "Good signature" in step 3 and "OK" in step 4 mean the download is authentic
and untampered. The trust anchor is the primary fingerprint in step 2: it matches
the key GitHub serves at `https://github.com/whykusanagi.gpg`, and the signing
subkey is certified under it.

The release also ships `manifest.json` + `manifest.json.asc` (full build metadata —
version, commit, checksums, URLs). Verify it the same way:
`gpg --verify manifest.json.asc manifest.json`.

## Launching on macOS

The macOS binary is ad-hoc signed (not yet notarized through Apple), so macOS
quarantines it on first download. After verifying above, clear the quarantine flag:

```bash
xattr -dr com.apple.quarantine ./celeste
```

You do this once. A future release may be Apple-notarized, which removes the step.
