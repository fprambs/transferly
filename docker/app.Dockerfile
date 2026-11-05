# Production image: builds the frontend, embeds it in the Go binary and ships a minimal non-root image.
ARG GO_VERSION=1.27.3
ARG NODE_VERSION=24
ARG PNPM_VERSION=12.16.1

FROM node:${NODE_VERSION}-trixie-slim AS web
ARG PNPM_VERSION
RUN npm install -g pnpm@${PNPM_VERSION}
WORKDIR /src/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM golang:${GO_VERSION}-trixie AS build
# The version of the build, the release tag such as v0.5.0; scripts/release.sh and the Compose files pass it.
ARG VERSION=dev
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
COPY backend/third_party/ ./third_party/
RUN go mod download
COPY backend/ ./
COPY --from=web /src/frontend/dist/ ./internal/transport/webui/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/fprambs/transferly/internal/version.Version=${VERSION}" -o /out/transferly ./cmd/transferly \
    && mkdir /out/data

FROM gcr.io/distroless/static-debian13@sha256:448810357cc8e67b7208988abdba0f5ec3d3aebee0a5b13eb624979792cffc0f
ARG VERSION=dev
LABEL org.opencontainers.image.title="Transferly" org.opencontainers.image.version="${VERSION}" org.opencontainers.image.source="https://github.com/fprambs/transferly"
COPY --from=build /out/transferly /transferly
COPY --from=build --chown=65532:65532 /out/data /data
ENV TRANSFERLY_DATA_DIR=/data
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=5 CMD ["/transferly", "healthcheck"]
ENTRYPOINT ["/transferly"]
