# Dockerfile for mock API server
FROM golang:1.21-alpine AS builder

WORKDIR /build

# Copy mock server code
COPY main.go .

# Build the mock server
RUN go build -o mock-server main.go

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates wget

WORKDIR /app

# Copy binary
COPY --from=builder /build/mock-server /app/mock-server

# Expose port
EXPOSE 8080

# Run the mock server
CMD ["/app/mock-server"]
