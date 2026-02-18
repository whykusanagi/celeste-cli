# IPFS Setup Guide

This guide explains how to configure and use the IPFS (InterPlanetary File System) skill in Celeste CLI for decentralized content storage and retrieval.

## ⚠️ Important: Provider Compatibility

The IPFS skill uses the official `go-ipfs-http-client` library which requires providers that support the **standard IPFS HTTP API**.

**✅ Currently Supported:**
- Infura IPFS (recommended - tested and working)
- Local IPFS nodes
- Any provider supporting standard IPFS HTTP API

**❌ Not Currently Supported:**
- Pinata (uses custom REST API, not standard IPFS HTTP API)
- Future enhancement planned for Pinata-specific implementation

## Overview

The IPFS skill allows you to:
- Upload content to IPFS (returns a Content Identifier - CID)
- Download content by CID
- Pin and unpin content for persistence
- List all pinned content
- Support for Infura IPFS and local IPFS nodes

## Supported Providers

### 1. Infura IPFS (Recommended)

Infura provides a hosted IPFS service with generous free tier limits.

**Getting Started:**
1. Sign up at [infura.io](https://infura.io)
2. Create a new project
3. Navigate to IPFS under your project
4. Copy your **Project ID** and **API Key Secret**

**Configuration:**
**Edit `~/.celeste/config.json`:**
```json
{
  "ipfs_provider": "infura",
  "ipfs_project_id": "YOUR_PROJECT_ID",
  "ipfs_api_secret": "YOUR_API_SECRET",
  "ipfs_gateway_url": "https://ipfs.infura.io",
  "ipfs_timeout_seconds": 30
}
```

### 2. Pinata (Future Enhancement)

⚠️ **Note**: Pinata is not currently supported as it uses a custom REST API instead of the standard IPFS HTTP API. A Pinata-specific implementation is planned for a future release.

For now, please use Infura IPFS or a local IPFS node.

### 3. Custom IPFS Node

Use your own IPFS node or a third-party gateway.

**Configuration:**
```json
{
  "ipfs_provider": "custom",
  "ipfs_gateway_url": "/ip4/127.0.0.1/tcp/5001",
  "ipfs_timeout_seconds": 30
}
```

## Configuration Methods

### Configuration File

Edit `~/.celeste/config.json`:
```json
{
  "ipfs_provider": "infura",
  "ipfs_project_id": "YOUR_PROJECT_ID",
  "ipfs_api_secret": "YOUR_API_SECRET",
  "ipfs_gateway_url": "https://ipfs.infura.io",
  "ipfs_timeout_seconds": 30
}
```

## Usage Examples

### Upload Content

#### Upload String Content
```bash
# Upload a string to IPFS
celeste skill ipfs --operation upload --content "Hello, decentralized world!"

# Returns:
{
  "success": true,
  "cid": "QmXxx...abc",
  "size": 28,
  "filename": "content.txt",
  "type": "content",
  "gateway_url": "https://ipfs.io/ipfs/QmXxx...abc",
  "message": "Successfully uploaded content to IPFS"
}
```

#### Upload File (Binary Files Supported)
```bash
# Upload an image file
celeste skill ipfs --operation upload --file_path /path/to/image.png

# Upload a PDF document
celeste skill ipfs --operation upload --file_path ~/documents/report.pdf

# Upload any binary file
celeste skill ipfs --operation upload --file_path ./data.zip

# Returns:
{
  "success": true,
  "cid": "QmYyy...def",
  "size": 524288,
  "filename": "image.png",
  "type": "file",
  "gateway_url": "https://ipfs.io/ipfs/QmYyy...def",
  "message": "Successfully uploaded file to IPFS"
}
```

**Supported File Types**:
- Images: PNG, JPG, GIF, SVG, WEBP
- Documents: PDF, DOCX, TXT, MD
- Archives: ZIP, TAR, GZ
- Audio/Video: MP3, MP4, AVI, MKV
- Binary: Any file type supported

**Note**: Either `--content` or `--file_path` must be provided, but not both.
```

### Download Content
```bash
# Download content by CID
celeste skill ipfs --operation download --cid QmXxx...abc

# Returns:
{
  "success": true,
  "cid": "QmXxx...abc",
  "content": "Hello, decentralized world!",
  "size": 28,
  "message": "Content successfully downloaded from IPFS"
}
```

### Pin Content
```bash
# Pin content to keep it available
celeste skill ipfs --operation pin --cid QmXxx...abc

# Returns:
{
  "success": true,
  "cid": "QmXxx...abc",
  "message": "Content successfully pinned on IPFS"
}
```

### Unpin Content
```bash
# Unpin content to free up storage
celeste skill ipfs --operation unpin --cid QmXxx...abc

# Returns:
{
  "success": true,
  "cid": "QmXxx...abc",
  "message": "Content successfully unpinned from IPFS"
}
```

### List Pins
```bash
# List all pinned content
celeste skill ipfs --operation list_pins

# Returns:
{
  "success": true,
  "pins": ["QmXxx...abc", "QmYyy...def", "QmZzz...ghi"],
  "count": 3,
  "message": "Found 3 pinned items"
}
```

## Use Cases

### 1. Decentralized Content Storage
Store files, images, or data permanently without relying on centralized servers.

### 2. NFT Metadata
Upload and retrieve NFT metadata and assets using content-addressed CIDs.

### 3. Data Sharing
Share content via immutable CIDs that anyone can access from IPFS gateways.

### 4. Backup and Archival
Create permanent backups of important data with cryptographic verification.

### 5. Distributed Applications
Build dApps that store and retrieve content from IPFS.

## Technical Details

### Libraries Used
- **go-ipfs-http-client** (v0.7.0) - Official IPFS HTTP API client
- **go-cid** (v0.6.0) - Content Identifier handling
- **go-multiaddr** (v0.9.0) - Multiaddr format for IPFS endpoints

### Authentication
- **Infura**: Basic Auth with `Project-ID:API-Secret` in base64
- **Pinata**: API key headers (`pinata_api_key`, `pinata_secret_api_key`)
- **Custom**: No authentication (local node)

### Content Identifiers (CIDs)
IPFS uses CIDs to uniquely identify content. CIDs are:
- **Content-addressed**: Same content = same CID
- **Immutable**: Content cannot be changed after upload
- **Cryptographically secure**: SHA-256 hashed

Example CID: `QmXnnyufdzAWL5CqZ2RnSNgPbvCc1ALT73s6epPrRnZ1Xy`

### Gateway URLs
Public IPFS gateways allow HTTP access to IPFS content:
- `https://ipfs.io/ipfs/<CID>`
- `https://cloudflare-ipfs.com/ipfs/<CID>`
- `https://gateway.pinata.cloud/ipfs/<CID>`

## Troubleshooting

### Error: "IPFS configuration is required"
**Solution**: Set the IPFS API key via environment variable or skills.json.

### Error: "Invalid CID"
**Solution**: Verify the CID format. CIDs should start with `Qm` (v0) or `baf` (v1).

### Error: "Failed to connect to IPFS"
**Solution**:
- Check your API credentials
- Verify network connectivity
- Ensure the IPFS endpoint is accessible
- For local nodes, ensure IPFS daemon is running (`ipfs daemon`)

### Error: "Timeout"
**Solution**: Increase `ipfs_timeout_seconds` in configuration. Large files may take longer.

### Error: "Content not found"
**Solution**: The CID may not be pinned or available on the network. Try pinning it first.

## Best Practices

1. **Pin Important Content**: Always pin content you want to keep available
2. **Use Unique CIDs**: CIDs are content-addressed, so duplicate content returns the same CID
3. **Gateway Selection**: Use reliable gateways (Infura, Pinata, Cloudflare) for production
4. **Backup CIDs**: Store CIDs in a database or file for later retrieval
5. **Monitor Storage**: Track pinned content to manage storage quotas

## Cost Considerations

### Infura Free Tier
- 5GB storage
- 100GB bandwidth per month
- Unlimited requests

### Pinata Free Tier
- 1GB storage
- Unlimited pins
- Unlimited gateway requests

### Self-Hosted
- Hardware/VPS costs
- Bandwidth costs
- Maintenance time

## Security Notes

- **Never commit API keys to version control**
- Use environment variables in CI/CD pipelines
- Rotate API keys periodically
- Monitor API usage for anomalies
- Use separate keys for development and production

## Further Reading

- [IPFS Documentation](https://docs.ipfs.io)
- [Infura IPFS Guide](https://docs.infura.io/infura/networks/ipfs)
- [Pinata Documentation](https://docs.pinata.cloud)
- [Content Addressing](https://docs.ipfs.io/concepts/content-addressing/)
