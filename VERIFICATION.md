# Verifying Celeste CLI Downloads

This guide explains how to verify the authenticity and integrity of Celeste CLI releases using GPG signatures.

## Why Verify?

Verifying downloads ensures that:
- The binary was built by the official maintainers
- The file hasn't been tampered with during download
- You're running authentic, unmodified code

## Quick Start

For most users, use the automated verification script:

```bash
# Download verification script
curl -O https://raw.githubusercontent.com/whykusanagi/celesteCLI/main/scripts/verify.sh
chmod +x verify.sh

# Verify a downloaded release
./verify.sh celeste-linux-amd64.tar.gz
```

The script will automatically:
1. Import the public key (if needed)
2. Verify the GPG signature
3. Verify the file checksum
4. Report the results

## Manual Verification

### Prerequisites

You'll need GPG installed:

**macOS**:
```bash
brew install gnupg
```

**Ubuntu/Debian**:
```bash
sudo apt-get install gnupg
```

**Windows**:
Download from [GnuPG.org](https://gnupg.org/download/)

### Step 1: Import Public Key

Choose one of the following methods to import the signing key:

**Option A: From Keybase (Recommended)**

Keybase provides verified identity with social proofs:

```bash
curl https://keybase.io/whykusanagi/pgp_keys.asc | gpg --import
```

**Option B: From GitHub**

GitHub serves keys for verified accounts:

```bash
curl https://github.com/whykusanagi.gpg | gpg --import
```

**Option C: From Key Server**

Public key servers provide decentralized distribution:

```bash
gpg --keyserver keys.openpgp.org --recv-keys 940490EF09DA31322BF7FD83875849AB1D541C55
```

### Step 2: Verify Key Fingerprint

After importing, **always** verify the fingerprint matches:

```bash
gpg --fingerprint 940490EF09DA31322BF7FD83875849AB1D541C55
```

Expected output:
```
pub   rsa4096 2025-12-04 [SC] [expires: 2041-11-30]
      9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55
uid           [ultimate] whykusanagi <me@whykusanagi.xyz>
sub   rsa4096 2025-12-04 [E] [expires: 2041-11-30]
```

**IMPORTANT**: The fingerprint must exactly match:
```
9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55
```

If it doesn't match, **DO NOT proceed**. Report this to security@whykusanagi.xyz.

### Step 3: Download Release Files

From the [releases page](https://github.com/whykusanagi/celesteCLI/releases/latest), download:

1. Your platform's binary archive (e.g., `celeste-linux-amd64.tar.gz`)
2. `checksums.txt` - SHA256 checksums for all binaries
3. `checksums.txt.asc` - GPG signature for checksums file

Optionally download:
- `manifest.json` - Complete release metadata
- `manifest.json.asc` - GPG signature for manifest
- `SIGNATURES.txt` - Information about the signatures

### Step 4: Verify GPG Signature

Verify the checksums file is signed by the correct key:

```bash
gpg --verify checksums.txt.asc checksums.txt
```

Expected output should include:
```
gpg: Signature made [DATE]
gpg:                using RSA key 940490EF09DA31322BF7FD83875849AB1D541C55
gpg: Good signature from "whykusanagi <me@whykusanagi.xyz>" [ultimate]
```

**Key indicators**:
- ✅ "Good signature" message
- ✅ Key ID matches: `940490EF09DA31322BF7FD83875849AB1D541C55`
- ✅ Name matches: "whykusanagi"

If you see warnings about trust levels, that's normal. The important part is the "Good signature" message and matching key ID.

### Step 5: Verify File Checksum

Check that your downloaded binary matches the signed checksums:

```bash
# macOS
shasum -a 256 --check --ignore-missing checksums.txt

# Linux
sha256sum --check --ignore-missing checksums.txt

# Windows (Git Bash)
sha256sum --check --ignore-missing checksums.txt
```

Expected output:
```
celeste-linux-amd64.tar.gz: OK
```

### Step 6: Extract and Use

Once verified, extract and install:

```bash
# Linux/macOS
tar xzf celeste-linux-amd64.tar.gz
sudo mv celeste-linux-amd64 /usr/local/bin/celeste
chmod +x /usr/local/bin/celeste

# Verify installation
celeste --version
```

## Verifying the Manifest (Optional)

The `manifest.json` file contains comprehensive metadata about the release:

```bash
# Verify manifest signature
gpg --verify manifest.json.asc manifest.json

# View manifest contents
cat manifest.json | jq .
```

The manifest includes:
- Release version and date
- Git commit hash
- Go version used for build
- All artifact checksums (SHA256 + SHA512)
- Download URLs
- Verification information

## Verification Failures

### What to Do if Verification Fails

**If GPG signature verification fails**:
1. ❌ **DO NOT use the downloaded file**
2. Re-download from the official GitHub releases page
3. Verify you imported the correct key (check fingerprint)
4. If it fails again, report to security@whykusanagi.xyz

**If checksum verification fails**:
1. ❌ **DO NOT use the downloaded file**
2. Re-download the binary archive
3. Re-download `checksums.txt` and `checksums.txt.asc`
4. Verify the GPG signature again
5. Re-check the checksum
6. If it fails again, report to security@whykusanagi.xyz

**Common errors**:

| Error | Meaning | Solution |
|-------|---------|----------|
| `gpg: BAD signature` | File was modified or signed with wrong key | Re-download, verify key fingerprint |
| `gpg: Can't check signature: No public key` | Key not imported | Import key using one of the three methods |
| `sha256sum: WARNING: ... computed checksum did NOT match` | File corrupted or modified | Re-download the binary |
| `gpg: WARNING: This key is not certified` | Your GPG hasn't marked key as trusted | Normal, verify fingerprint manually |

## Key Information

- **Key ID**: `875849AB1D541C55`
- **Fingerprint**: `940490EF09DA31322BF7FD83875849AB1D541C55`
- **Key Type**: RSA 4096-bit
- **Created**: 2025-12-04
- **Expires**: 2041-11-30
- **Owner**: whykusanagi <me@whykusanagi.xyz>
- **Keybase**: [@whykusanagi](https://keybase.io/whykusanagi)
- **GitHub**: [@whykusanagi](https://github.com/whykusanagi)

### Key Distribution Points

The same key is available from multiple trusted sources:

1. **Keybase**: https://keybase.io/whykusanagi/pgp_keys.asc
2. **GitHub**: https://github.com/whykusanagi.gpg
3. **Key Servers**: `keys.openpgp.org`, `pgp.mit.edu`
4. **Repository**: `keys/public-key.asc` (in this repository)

All sources should serve the identical key with fingerprint `3F4E 9533 810C 9989 60B9  48CD 3DDF A887 09FE 468B`.

## Automated Verification Script

The `scripts/verify.sh` script automates all verification steps:

**Features**:
- Automatic key import from Keybase, GitHub, or key servers
- GPG signature verification with clear success/failure messages
- Checksum verification
- Colored output for readability
- No external dependencies except GPG and curl

**Usage**:
```bash
./scripts/verify.sh celeste-linux-amd64.tar.gz
```

**Script output example**:
```
ℹ Verifying celeste-linux-amd64.tar.gz
✓ GPG key already imported
✓ Downloading checksums.txt and checksums.txt.asc
ℹ Verifying GPG signature...
✓ GPG signature verified
ℹ Verifying file checksum...
✓ Checksum verified
✓ All verifications passed! celeste-linux-amd64.tar.gz is authentic.
```

## Trust Model

Celeste CLI uses a multi-layered trust model:

1. **Code Signing**: All commits are GPG-signed
2. **Release Signing**: All release artifacts are GPG-signed
3. **Checksum Signing**: Checksums file is GPG-signed
4. **Identity Verification**: Key is linked to verified social accounts via Keybase
5. **Transparency**: Full source code available on GitHub
6. **CI/CD Verification**: GitHub Actions builds are auditable

## Building from Source

For maximum trust, build from source:

```bash
# Clone repository
git clone https://github.com/whykusanagi/celesteCLI.git
cd celesteCLI

# Verify signed commit
git log --show-signature -1

# Build
make build

# Install
make install
```

All commits to `main` branch are GPG-signed.

## Security Contact

- **Security Issues**: security@whykusanagi.xyz
- **General Questions**: [GitHub Issues](https://github.com/whykusanagi/celesteCLI/issues)
- **Security Policy**: [SECURITY.md](https://github.com/whykusanagi/celesteCLI/blob/main/SECURITY.md)

## Appendix: Key Management Best Practices

### Verify the Key Before First Use

Before trusting any public key:
1. Import from multiple sources (Keybase, GitHub, key servers)
2. Compare fingerprints from all sources
3. Check Keybase social proofs
4. Verify GitHub account is official maintainer
5. Check key age and expiration

### Keep GPG Updated

```bash
# macOS
brew upgrade gnupg

# Ubuntu/Debian
sudo apt-get update && sudo apt-get upgrade gnupg
```

### Refresh Keys Periodically

```bash
gpg --refresh-keys
```

This checks for key expiration updates and revocations.

## Frequently Asked Questions

**Q: Why do I see "WARNING: This key is not certified with a trusted signature"?**

A: This is normal. It means you haven't personally marked the key as "ultimately trusted" in your GPG keyring. The important check is the fingerprint and the "Good signature" message.

**Q: Can I skip verification?**

A: Not recommended. Verification ensures you're running official code. It takes less than 1 minute with the automated script.

**Q: What if the key expires?**

A: The current key expires 2027-03-25. Before expiration, we'll extend it or issue a new key. Check SECURITY.md for updates.

**Q: Do I need to verify every time I download?**

A: Yes. Always verify every download, even updates. This protects against compromised mirrors or man-in-the-middle attacks.

**Q: Is the automated script safe to use?**

A: Yes. Review it first at `scripts/verify.sh`. It only imports keys, verifies signatures, and checks checksums.

---

**Last Updated**: 2025-12-07
**Key Expires**: 2027-03-25
