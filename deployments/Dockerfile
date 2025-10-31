# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY main.go ./
COPY internal/ ./internal/

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o latex-compile .

# Runtime stage
FROM ubuntu:22.04

# Prevent timezone prompts
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC

# Install TexLive and dependencies
RUN apt-get update && apt-get install -y \
    texlive-latex-base \
    texlive-latex-extra \
    texlive-fonts-recommended \
    texlive-fonts-extra \
    texlive-science \
    texlive-publishers \
    texlive-bibtex-extra \
    biber \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/latex-compile .

# Create logs directory
RUN mkdir -p /app/logs

# Set environment variables
ENV PORT=3001
ENV HISTORY_DIR=/app/logs
ENV GIN_MODE=release

# Expose port
EXPOSE 3001

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3001/health || exit 1

# Run
CMD ["./latex-compile"]

