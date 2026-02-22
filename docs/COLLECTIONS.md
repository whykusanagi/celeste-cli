# Collections Guide

## Overview

Collections enable Celeste to search your custom documents during conversations. Upload documentation, notes, lore, or any text-based knowledge, and Celeste will automatically search these documents when answering questions.

**Key Features:**
- Semantic search across uploaded documents
- Support for multiple document formats (.md, .txt, .pdf, .html)
- Interactive TUI for collection management
- Automatic tool integration with xAI Grok

**Powered by:** xAI Collections API with RAG (Retrieval-Augmented Generation)

---

## Backend Architecture

**Collections use the native xAI backend**, which provides:
- Direct API control for full Collections support
- Automatic `collection_ids` parameter injection
- Real-time tracking of sources used (`num_sources_used`)
- Seamless integration with tool calling

When you use a config with xAI URLs (e.g., `config.grok.json`), Celeste automatically:
1. Detects the xAI provider from the base URL (`https://api.x.ai/v1`)
2. Switches to the native xAI backend
3. Enables Collections if configured
4. Passes active collection IDs with every chat request

You'll see in the logs:
```
Using xAI backend with Collections support
xAI Collections enabled: 1 collections active: [collection_abc123...]
✅ xAI Collections: 3 sources used in response
```

---

## Quick Start

### 1. Get a Management API Key

Collections require an xAI Management API Key (separate from your Chat API key).

1. Visit [https://console.x.ai](https://console.x.ai)
2. Navigate to API Keys
3. Create a new Management API Key with `AddFileToCollection` permission
4. Copy the key (starts with `xai-`)

### 2. Configure the Key

```bash
# Option 1: Save to config
celeste config --set-management-key xai-YOUR-MANAGEMENT-KEY

# Option 2: Environment variable
export XAI_MANAGEMENT_API_KEY=xai-YOUR-MANAGEMENT-KEY
```

### 3. Create a Collection

```bash
celeste collections create "my-docs" --description "My documentation and notes"
```

Output:
```
✅ Collection created successfully!
   Collection ID: collection_abc123def
   Name: my-docs
   Description: My documentation and notes

Next steps:
  1. Upload documents: celeste collections upload collection_abc123def <files>
  2. Enable for chat: celeste collections enable collection_abc123def
```

### 4. Upload Documents

```bash
# Upload single file
celeste collections upload collection_abc123def document.md

# Upload multiple files
celeste collections upload collection_abc123def docs/*.md

# Upload directory recursively
celeste collections upload collection_abc123def docs/ --recursive
```

### 5. Enable for Chat

```bash
celeste collections enable collection_abc123def
```

### 6. Start Chatting

```bash
celeste chat
```

Celeste will now automatically search your documents when relevant to your questions!

---

## CLI Commands Reference

### `celeste collections create <name>`

Create a new collection.

**Options:**
- `--description, -d` - Collection description

**Example:**
```bash
celeste collections create "celeste-lore" --description "Celeste personality and backstory"
```

### `celeste collections list`

List all collections with metadata.

**Example:**
```bash
celeste collections list
```

**Output:**
```
Collections (2):

✓ [1] celeste-lore
    ID: collection_abc123
    Description: Celeste personality and backstory
    Documents: 5
    Created: 2026-02-17 10:30:00

  [2] programming-docs
    ID: collection_def456
    Description: Programming documentation
    Documents: 15
    Created: 2026-02-16 14:20:00

✓ = Active (enabled for chat)
```

### `celeste collections upload <collection-id> <files...>`

Upload documents to a collection.

**Options:**
- `--recursive, -r` - Upload directories recursively

**Supported Formats:**
- Markdown (`.md`)
- Text (`.txt`)
- PDF (`.pdf`)
- HTML (`.html`, `.htm`)

**Size Limit:** 10MB per file

**Examples:**
```bash
# Single file
celeste collections upload collection_abc123 lore.md

# Multiple files
celeste collections upload collection_abc123 docs/*.md

# Directory recursively
celeste collections upload collection_abc123 knowledge-base/ --recursive
```

### `celeste collections enable <collection-id>`

Add a collection to the active set (enabled for chat).

**Example:**
```bash
celeste collections enable collection_abc123
```

### `celeste collections disable <collection-id>`

Remove a collection from the active set.

**Example:**
```bash
celeste collections disable collection_abc123
```

### `celeste collections delete <collection-id>`

Delete a collection and all its documents.

**Options:**
- `--force, -f` - Skip confirmation prompt

**Example:**
```bash
celeste collections delete collection_abc123
```

### `celeste collections show <collection-id>`

Display detailed information about a collection.

**Example:**
```bash
celeste collections show collection_abc123
```

---

## TUI Interface

Access the interactive collections manager from within chat:

```
/collections
```

**Keybindings:**
- `↑/↓` or `k/j` - Navigate collections
- `Space` - Toggle collection active/inactive
- `q` or `Esc` - Return to chat

**Features:**
- Visual indicator for active collections (✓)
- Real-time toggle without leaving TUI
- Document counts at a glance

---

## Best Practices

### Organizing Collections

**Use separate collections for different knowledge domains:**

```bash
# Create domain-specific collections
celeste collections create "celeste-personality" --description "Celeste's character and lore"
celeste collections create "project-docs" --description "Current project documentation"
celeste collections create "reference" --description "General reference materials"
```

**Benefits:**
- Easy to enable/disable specific knowledge domains
- Better organization and maintenance
- More focused search results

### Document Preparation

**For best results:**

1. **Use clear headings** - Helps with semantic chunking
2. **Include context** - Don't assume prior knowledge
3. **Keep documents focused** - One topic per document works best
4. **Use descriptive filenames** - Makes management easier

**Example structure:**
```markdown
# Topic Name

## Overview
Brief introduction to the topic.

## Key Concepts
- Concept 1: Explanation
- Concept 2: Explanation

## Examples
Practical examples with context.
```

### Managing Active Collections

**Enable only what you need:**
```bash
# For a coding session
celeste collections enable programming-docs
celeste collections disable celeste-personality

# For creative writing
celeste collections enable celeste-personality
celeste collections disable programming-docs
```

**Why:** Active collections improve response speed and relevance.

---

## Troubleshooting

### "xAI Management API key not configured"

**Problem:** No management key set.

**Solution:**
```bash
celeste config --set-management-key xai-YOUR-KEY
# OR
export XAI_MANAGEMENT_API_KEY=xai-YOUR-KEY
```

### "unsupported format" when uploading

**Problem:** File format not supported or file too large.

**Solution:**
- Check file extension (must be .md, .txt, .pdf, or .html)
- Verify file size is under 10MB
- Convert unsupported formats to text or markdown

### Collections not being searched in chat

**Checklist:**
1. ✓ Collection is enabled: `celeste collections list` (check for ✓)
2. ✓ Using xAI provider (Grok)
3. ✓ Documents uploaded successfully
4. ✓ Questions are relevant to uploaded content

**Enable collection:**
```bash
celeste collections enable <collection-id>
```

### Upload fails with connection error

**Problem:** Network connectivity or API issue.

**Solution:**
1. Check internet connection
2. Verify API key has correct permissions
3. Check xAI API status
4. Retry upload

---

## Advanced Usage

### Batch Operations

**Upload multiple collections:**
```bash
# Script for bulk upload
for dir in docs/*/; do
  name=$(basename "$dir")
  id=$(celeste collections create "$name" | grep "Collection ID" | awk '{print $3}')
  celeste collections upload "$id" "$dir" --recursive
  celeste collections enable "$id"
done
```

### Integration with Git Hooks

**Auto-update collection on git pull:**

`.git/hooks/post-merge`:
```bash
#!/bin/bash
COLLECTION_ID="collection_abc123"
celeste collections upload "$COLLECTION_ID" docs/ --recursive
```

### Context-Specific Collections

**Use different collections for different projects:**

```bash
# Switch to project-specific setup
celeste collections disable all  # (disable individually)
celeste collections enable project-xyz-docs
celeste config use project-xyz  # If using named configs
```

---

## API Integration

Collections use the xAI Management API and Chat Completions API:

**Management API (for CRUD):**
- Create collections
- Upload documents
- Delete collections

**Chat Completions API (for search):**
- Automatic `collections_search` tool when collections enabled
- Server-side execution (no client-side tool handling needed)

**See:** xAI API Documentation for full API reference

---

## Limitations

**Current Limitations:**
- xAI provider only (Grok models)
- 10MB file size limit per document
- Supported formats: .md, .txt, .pdf, .html

**Planned:**
- Additional document formats
- Document versioning
- Collection sharing

---

## FAQ

**Q: Do I need a separate API key for collections?**
A: Yes, collections require an xAI Management API Key with collection permissions.

**Q: Can I use collections with other providers (OpenAI, Claude, etc.)?**
A: No, collections are currently xAI/Grok only.

**Q: How many collections can I have?**
A: Check your xAI account limits. Typically generous for personal use.

**Q: Are my documents stored securely?**
A: Documents are stored on xAI's servers. Review xAI's privacy policy for details.

**Q: Can I share collections with others?**
A: Not currently. Each collection is tied to your API key.

**Q: How do I update a document?**
A: Re-upload the file with the same name, or delete and re-upload.

**Q: Will enabling many collections slow down responses?**
A: Enable only relevant collections for best performance. xAI handles the search efficiently, but more collections means more content to search.

---

## Examples

### Example 1: Personal Knowledge Base

```bash
# Create and populate personal knowledge base
celeste collections create "knowledge-base" --description "Personal notes and references"

# Upload various documents
celeste collections upload <id> ~/Documents/notes/*.md --recursive
celeste collections upload <id> ~/Documents/references/*.pdf

# Enable for all chats
celeste collections enable <id>
```

### Example 2: Project Documentation

```bash
# Create project-specific collection
celeste collections create "project-alpha" --description "Project Alpha documentation"

# Upload project docs
celeste collections upload <id> ./docs/ --recursive

# Enable only when working on this project
celeste collections enable <id>
```

### Example 3: Learning Material

```bash
# Create learning collection
celeste collections create "learning" --description "Tutorials and learning resources"

# Upload course materials
celeste collections upload <id> ~/Courses/web-dev/*.md
celeste collections upload <id> ~/Courses/web-dev/resources/*.html --recursive

# Enable for study sessions
celeste collections enable <id>
```

---

## Support

For issues or feature requests:
- GitHub Issues: https://github.com/whykusanagi/celeste-cli/issues
- Documentation: https://github.com/whykusanagi/celeste-cli/docs

---

**Happy documenting! 📚**
