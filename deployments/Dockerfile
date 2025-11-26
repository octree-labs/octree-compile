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

ARG TEXLIVE_YEAR=2025

# Prevent timezone prompts and configure TeX Live paths
ENV DEBIAN_FRONTEND=noninteractive \
    TZ=UTC \
    PATH=/usr/local/texlive/${TEXLIVE_YEAR}/bin/x86_64-linux:$PATH \
    TEXMFHOME=/root/texmf

# Install TeX Live from TUG (2025) along with system fonts
RUN set -eux; \
    apt-get update && apt-get install -y \
      perl \
      wget \
      xz-utils \
      fontconfig \
      ca-certificates \
      curl \
      unzip \
      fonts-ubuntu \
      fonts-noto-cjk \
      fonts-noto-color-emoji \
      && rm -rf /var/lib/apt/lists/*; \
    mkdir -p /tmp/install-texlive && cd /tmp/install-texlive; \
    curl -L -o install-tl-unx.tar.gz https://mirrors.ctan.org/systems/texlive/tlnet/install-tl-unx.tar.gz; \
    tar -xzf install-tl-unx.tar.gz --strip-components=1; \
    printf '%s\n' \
      "selected_scheme scheme-full" \
      "TEXDIR /usr/local/texlive/${TEXLIVE_YEAR}" \
      "TEXMFCONFIG /root/.texlive${TEXLIVE_YEAR}/texmf-config" \
      "TEXMFVAR /root/.texlive${TEXLIVE_YEAR}/texmf-var" \
      "TEXMFHOME /root/texmf" \
      "option_doc 0" \
      "option_src 0" \
      "option_letter 0" > texlive.profile; \
    ./install-tl --profile texlive.profile; \
    /usr/local/texlive/${TEXLIVE_YEAR}/bin/x86_64-linux/tlmgr option repository https://mirrors.ctan.org/systems/texlive/tlnet; \
    /usr/local/texlive/${TEXLIVE_YEAR}/bin/x86_64-linux/tlmgr update --self --all; \
    /usr/local/texlive/${TEXLIVE_YEAR}/bin/x86_64-linux/tlmgr install \
      latexmk \
      biber \
      fontspec \
      unicode-math \
      libertine \
      simpleicons \
      fontawesome5 \
      cabin \
      cochineal \
      arphic; \
    /usr/local/texlive/${TEXLIVE_YEAR}/bin/x86_64-linux/tlmgr path add; \
    fc-cache -fv; \
    cd / && rm -rf /tmp/install-texlive

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/latex-compile .

# Copy cleanup script
COPY scripts/cleanup-logs.sh /app/scripts/cleanup-logs.sh
RUN chmod +x /app/scripts/cleanup-logs.sh

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

