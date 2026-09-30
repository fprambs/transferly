# Development tools image: Go, Node, pnpm, code generators and linters.
# It is the only place development tools live; the host installs nothing.
# Pinned versions are listed in docs/guides/README.md.
ARG GO_VERSION=1.27
ARG NODE_VERSION=24

FROM golang:${GO_VERSION}-bookworm AS go

FROM node:${NODE_VERSION}-bookworm
COPY --from=go /usr/local/go /usr/local/go

ENV GOROOT=/usr/local/go \
    GOPATH=/cache/go \
    GOMODCACHE=/cache/gomod \
    GOCACHE=/cache/gobuild \
    GOFLAGS=-buildvcs=false \
    PNPM_HOME=/cache/pnpm \
    pnpm_config_store_dir=/cache/pnpm-store \
    HOME=/cache/home \
    PATH=/usr/local/go/bin:/usr/local/bin:/cache/pnpm:$PATH

# Cache directories writable by any UID; named volumes inherit these permissions.
RUN mkdir -p /cache/go /cache/gomod /cache/gobuild /cache/pnpm /cache/pnpm-store /cache/home \
    && chmod -R 0777 /cache

# ffmpeg, 7-Zip, bzip2, xz and zstd generate the media and archive samples of the file viewers (testenv/preview-samples). Package downloads use HTTPS, with
# retries, so a middlebox cannot alter them (see testenv/README.md).
RUN sed -i 's|http://|https://|g' /etc/apt/sources.list.d/debian.sources \
    && printf 'Acquire::Retries "5";\n' > /etc/apt/apt.conf.d/80-retries \
    && apt-get update \
    && apt-get install -y --no-install-recommends --fix-missing ffmpeg p7zip-full bzip2 xz-utils zstd \
    && rm -rf /var/lib/apt/lists/*

# Go tools, pinned by version.
ARG SQLC_VERSION=v1.31.1
ARG GOOSE_VERSION=v3.28.0
ARG OAPI_CODEGEN_VERSION=v2.8.0
ARG GOVULNCHECK_VERSION=v1.8.0
ARG AIR_VERSION=v1.67.4
ARG GOLANGCI_LINT_VERSION=v2.14.0
RUN GOBIN=/usr/local/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION} \
    && GOBIN=/usr/local/bin go install github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION} \
    && GOBIN=/usr/local/bin go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@${OAPI_CODEGEN_VERSION} \
    && GOBIN=/usr/local/bin go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} \
    && GOBIN=/usr/local/bin go install github.com/air-verse/air@${AIR_VERSION} \
    && GOBIN=/usr/local/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} \
    && chmod -R a+rX /cache/gomod && rm -rf /cache/gomod/* /cache/gobuild/* && chmod -R 0777 /cache

# pnpm installed with npm and pinned: no runtime downloads, works with any UID.
ARG PNPM_VERSION=12.6.0
RUN npm install -g pnpm@${PNPM_VERSION} && pnpm --version

WORKDIR /workspace
USER node
CMD ["bash"]
