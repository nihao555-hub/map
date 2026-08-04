# Build stage for Playwright dependencies
FROM ubuntu:20.04 AS playwright-deps
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/browsers
ENV PLAYWRIGHT_DRIVER_PATH=/opt/ms-playwright-go
ARG TARGETARCH
ARG PLAYWRIGHT_GO_VERSION=v0.6100.0
ARG GOPROXY=https://proxy.golang.org,direct
ARG PLAYWRIGHT_DOWNLOAD_HOST=
ENV GOPROXY=${GOPROXY}
ENV PLAYWRIGHT_DOWNLOAD_HOST=${PLAYWRIGHT_DOWNLOAD_HOST}

RUN export PATH=$PATH:/usr/local/go/bin:/root/go/bin \
    && apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl wget \
    && if [ "$TARGETARCH" = "arm64" ]; then \
         GO_ARCH="arm64"; \
       else \
         GO_ARCH="amd64"; \
       fi \
    && wget -q "https://go.dev/dl/go1.26.5.linux-${GO_ARCH}.tar.gz" \
    && tar -C /usr/local -xzf "go1.26.5.linux-${GO_ARCH}.tar.gz" \
    && rm "go1.26.5.linux-${GO_ARCH}.tar.gz" \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && go install github.com/mxschmitt/playwright-go/cmd/playwright@${PLAYWRIGHT_GO_VERSION} \
    && mkdir -p /opt/browsers \
    && playwright install chromium --with-deps

# Build stage
FROM golang:1.26.5-trixie AS builder
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /app
COPY go.mod go.sum ./
COPY scrapemate-patched ./scrapemate-patched
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -mod=mod -ldflags="-w -s" -o /usr/bin/google-maps-scraper

# Final stage
FROM debian:trixie-slim
ARG APT_MIRROR=
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/browsers
ENV PLAYWRIGHT_DRIVER_PATH=/opt/ms-playwright-go

# Install only the necessary dependencies in a single layer
RUN if [ -n "$APT_MIRROR" ]; then \
      sed -i "s|http://deb.debian.org|http://$APT_MIRROR|g; s|http://security.debian.org|http://$APT_MIRROR|g" \
        /etc/apt/sources.list.d/debian.sources 2>/dev/null || true; \
    fi \
    && apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    python3 \
    libnss3 \
    libnspr4 \
    libatk1.0-0 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdrm2 \
    libdbus-1-3 \
    libxkbcommon0 \
    libatspi2.0-0 \
    libx11-6 \
    libxcomposite1 \
    libxdamage1 \
    libxext6 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libpango-1.0-0 \
    libcairo2 \
    libasound2 \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY --from=playwright-deps /opt/browsers /opt/browsers
COPY --from=playwright-deps /opt/ms-playwright-go /opt/ms-playwright-go

RUN chmod -R 755 /opt/browsers \
    && chmod -R 755 /opt/ms-playwright-go

COPY --from=builder /usr/bin/google-maps-scraper /usr/bin/

ENTRYPOINT ["google-maps-scraper"]
