# Multi-stage Dockerfile for running CelesteCLI tests
# Usage: docker compose up --build

# Stage 1: Build test binaries
FROM golang:1.26-alpine AS builder

LABEL maintainer="whykusanagi"
LABEL description="CelesteCLI test environment"

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files first (for caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build test binaries for all packages
RUN mkdir -p /tmp/tests && \
    go test -c -o /tmp/tests/config_test ./cmd/celeste/config && \
    go test -c -o /tmp/tests/llm_test ./cmd/celeste/llm && \
    go test -c -o /tmp/tests/tools_test ./cmd/celeste/tools && \
    go test -c -o /tmp/tests/tools_builtin_test ./cmd/celeste/tools/builtin && \
    go test -c -o /tmp/tests/permissions_test ./cmd/celeste/permissions && \
    go test -c -o /tmp/tests/codegraph_test ./cmd/celeste/codegraph && \
    go test -c -o /tmp/tests/context_test ./cmd/celeste/context && \
    go test -c -o /tmp/tests/grimoire_test ./cmd/celeste/grimoire && \
    go test -c -o /tmp/tests/costs_test ./cmd/celeste/costs && \
    go test -c -o /tmp/tests/hooks_test ./cmd/celeste/hooks && \
    go test -c -o /tmp/tests/memories_test ./cmd/celeste/memories && \
    go test -c -o /tmp/tests/sessions_test ./cmd/celeste/sessions && \
    go test -c -o /tmp/tests/checkpoints_test ./cmd/celeste/checkpoints && \
    go test -c -o /tmp/tests/planning_test ./cmd/celeste/planning && \
    go test -c -o /tmp/tests/server_test ./cmd/celeste/server && \
    echo "Test binaries built successfully"

# Verify binaries exist
RUN ls -la /tmp/tests/

# Stage 2: Test runner
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache bash curl jq ca-certificates tzdata

# Create celeste user for running tests (non-root)
RUN addgroup -g 1000 celeste && \
    adduser -D -u 1000 -G celeste celeste

# Set working directory
WORKDIR /app

# Copy test binaries from builder (as celeste user)
COPY --chown=celeste:celeste --from=builder /tmp/tests /app/tests

# Copy test fixtures and scripts (as celeste user)
COPY --chown=celeste:celeste test/fixtures /app/fixtures
COPY --chown=celeste:celeste test/scripts /app/scripts

# Create output directories and set ALL permissions as celeste user
RUN mkdir -p /app/reports /app/coverage && \
    chmod +x /app/scripts/*.sh && \
    chown -R celeste:celeste /app && \
    ls -la /app && \
    ls -la /app/reports

# Switch to non-root user
USER celeste

# Environment variables for testing
ENV CGO_ENABLED=0
ENV GO_ENV=test

# Default command: run all tests
CMD ["/bin/bash", "/app/scripts/run-all-tests.sh"]
