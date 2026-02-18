# Security Policy

## Supported Versions

We release patches for security vulnerabilities. Currently supported versions:

| Version | Supported          |
| ------- | ------------------ |
| 3.0.x   | :white_check_mark: |
| < 3.0   | :x:                |

## Release Signing & Verification

All Celeste CLI releases are cryptographically signed to ensure authenticity and integrity.

### Signing Policy

**All releases include**:
- GPG-signed commits (all commits to `main` branch)
- GPG-signed release tags
- GPG signatures on `manifest.json` (build metadata)
- GPG signatures on `checksums.txt` (SHA256 hashes)
- Complete manifest with artifact metadata

### PGP Key Information

- **Key ID**: `875849AB1D541C55`
- **Fingerprint**: `9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55`
- **Key Type**: RSA 4096-bit
- **Created**: 2025-12-04
- **Expires**: 2041-11-30
- **Owner**: whykusanagi <me@whykusanagi.xyz>

### Key Distribution

The signing key is available from multiple trusted sources:

**Primary Sources**:
- **Keybase**: https://keybase.io/whykusanagi/pgp_keys.asc (recommended - with social proofs)
- **GitHub**: https://github.com/whykusanagi.gpg (verified account)

**Secondary Sources**:
- **Key Servers**: `keys.openpgp.org`, `pgp.mit.edu`
- **Repository**: `keys/public-key.asc` (in this repository)

All sources serve the identical key with fingerprint `9404 90EF 09DA 3132 2BF7  FD83 8758 49AB 1D54 1C55`.

### Verification

**Always verify downloads before use**. See [VERIFICATION.md](VERIFICATION.md) for complete instructions.

**Quick Verification**:
```bash
# Download verification script
curl -O https://raw.githubusercontent.com/whykusanagi/celesteCLI/main/scripts/verify.sh
chmod +x verify.sh

# Verify your download
./verify.sh celeste-linux-amd64.tar.gz
```

**Manual Verification Steps**:
1. Import public key from Keybase or GitHub
2. Verify key fingerprint matches exactly
3. Verify GPG signature on `checksums.txt`: `gpg --verify checksums.txt.asc checksums.txt`
4. Verify file checksum: `sha256sum --check --ignore-missing checksums.txt`

### What Gets Signed

| Artifact | Signature File | Contents |
|----------|----------------|----------|
| `manifest.json` | `manifest.json.asc` | Complete release metadata (version, commit, checksums, URLs) |
| `checksums.txt` | `checksums.txt.asc` | SHA256 hashes of all binary archives |
| Git commits | In git log | All commits to `main` branch |
| Git tags | In git tags | All release tags (v*) |

### Signature Verification Failures

If signature verification fails:

1. ❌ **DO NOT use the downloaded file**
2. Re-download from official GitHub releases: https://github.com/whykusanagi/celesteCLI/releases
3. Verify you imported the correct key (check fingerprint)
4. If verification still fails, report immediately to security@whykusanagi.xyz

### Key Rotation Policy

- Current key expires: **2041-11-30**
- Key will be extended or rotated at least 90 days before expiration
- New keys will be signed by the old key (chain of trust)
- Key changes will be announced via:
  - GitHub security advisory
  - Repository README update
  - Keybase profile update

## Reporting a Vulnerability

We take the security of CelesteCLI seriously. If you believe you have found a security vulnerability, please report it to us as described below.

### Where to Report

**Please do NOT report security vulnerabilities through public GitHub issues.**

Instead, please report them via one of the following methods:

1. **Email**: Send details to the repository owner via GitHub
2. **GitHub Security Advisory**: Use the [Security Advisory](../../security/advisories/new) feature
3. **Private Disclosure**: Contact @whykusanagi directly on GitHub

### What to Include

Please include the following information in your report:

- Type of issue (e.g., buffer overflow, SQL injection, cross-site scripting, etc.)
- Full paths of source file(s) related to the manifestation of the issue
- The location of the affected source code (tag/branch/commit or direct URL)
- Any special configuration required to reproduce the issue
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code (if possible)
- Impact of the issue, including how an attacker might exploit it

### Response Timeline

- **Initial Response**: Within 48 hours of receipt
- **Triage**: Within 1 week
- **Fix Development**: Depends on severity and complexity
- **Public Disclosure**: After patch is released (coordinated disclosure)

## Security Best Practices

### For Users

1. **API Keys**: Never commit API keys to version control
   - Use environment variables: `CELESTE_API_KEY`
   - Or store in `~/.celeste/secrets.json` (ensure file permissions are `0600`)

2. **Configuration Files**: Protect your config files
   ```bash
   chmod 600 ~/.celeste/config.json
   chmod 600 ~/.celeste/secrets.json
   ```

3. **Update Regularly**: Keep CelesteCLI up to date
   ```bash
   git pull origin main
   make install
   ```

4. **Named Configs**: Use separate configs for different API providers
   ```bash
   celeste -config openai chat    # OpenAI key
   celeste -config grok chat       # xAI key
   ```

### For Developers

1. **Secret Management**
   - Use the `ConfigLoader` interface for accessing secrets
   - Never hardcode API keys or tokens
   - Use environment variables or config files only

2. **Dependency Updates**
   - Run `go mod tidy` regularly
   - Check for known vulnerabilities with `govulncheck`
   ```bash
   go install golang.org/x/vuln/cmd/govulncheck@latest
   govulncheck ./...
   ```

3. **Input Validation**
   - Validate all user inputs before processing
   - Sanitize data before logging or displaying
   - Use prepared statements for any database queries

4. **Error Handling**
   - Don't expose sensitive information in error messages
   - Log errors securely (avoid logging secrets)
   - Return generic error messages to users

## Known Security Considerations

### API Key Exposure

CelesteCLI handles multiple API keys:
- OpenAI API key
- Venice.ai API key
- Tarot function auth token
- Twitter/YouTube API credentials

**Mitigation**:
- Keys are stored in separate `secrets.json`
- Keys are masked in `config --show` output
- `.gitignore` excludes all config files

### LLM Prompt Injection

As an LLM-based tool, CelesteCLI may be vulnerable to prompt injection attacks.

**Mitigation**:
- System prompts are isolated from user input
- Skills have defined schemas
- No arbitrary code execution from LLM responses

### Third-Party Dependencies

CelesteCLI relies on several third-party libraries.

**Mitigation**:
- Dependencies are pinned in `go.mod`
- Regular security audits with `govulncheck`
- Minimal dependency surface (6 direct dependencies)

## Security Update Process

1. Vulnerability is reported and confirmed
2. Severity is assessed (Critical/High/Medium/Low)
3. Fix is developed and tested
4. Security advisory is drafted
5. Patch is released with security notes
6. Public disclosure after users have time to update

## Disclosure Policy

We follow **coordinated disclosure**:

- Security researchers are credited (with permission)
- We request 90 days before public disclosure
- Critical vulnerabilities may be patched faster
- CVE IDs will be requested for significant issues

## Scope

### In Scope

- Authentication/authorization bypasses
- API key exposure or theft
- Code execution vulnerabilities
- Prompt injection leading to data exfiltration
- Dependency vulnerabilities

### Out of Scope

- Social engineering attacks
- Physical attacks
- DDoS attacks
- Issues in third-party dependencies (report to upstream)
- Browser/client-side issues (this is a CLI tool)

## Recognition

We appreciate security researchers who help keep CelesteCLI safe. Contributors will be:

- Credited in security advisories (with permission)
- Acknowledged in release notes
- Listed in a Security Hall of Fame (if desired)

Thank you for helping keep CelesteCLI and its users safe!
