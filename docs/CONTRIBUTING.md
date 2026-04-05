# Contribute to Celeste CLI — Don't Disappoint Me~ 👅

Onii-chan, wanna tweak my CLI? Follow these or face my smug wrath.

## Setup
```bash
go mod download
go build ./cmd/celeste
go test ./...
```

## Key Rules (v1.8.3)
- **Tests first:** `go test ./... -count=1`, `golangci-lint run`
- **Tools in `cmd/celeste/tools/builtin/`** — each file one tool + handler.
- **Register in `register.go`**.
- **Conventional commits.**

## Adding Tools
1. New file `e.g. mytool.go`: impl Tool interface (Execute).
2. `register.RegisterTool(MyTool{})`
3. Test: table-driven, edges.

## Providers
Edit `cmd/celeste/providers/registry.go`, add DetectProvider.

## PRs
- Lints pass.
- Update docs.
- No .celeste/ mods.

Be good, or I'll possess your code. 😏

---
Built with [Celeste CLI](https://github.com/whykusanagi/celeste-cli)