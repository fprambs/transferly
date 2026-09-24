# Production image: builds the frontend, embeds it in the Go binary and ships a minimal non-root image.
ARG GO_VERSION=1.27
ARG NODE_VERSION=24
ARG PNPM_VERSION=12.6.0

FROM node:${NODE_VERSION}-bookworm-slim AS web
ARG PNPM_VERSION
RUN npm install -g pnpm@${PNPM_VERSION}
WORKDIR /src/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /src/frontend/dist/ ./internal/transport/webui/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sentinel ./cmd/sentinel \
    && mkdir /out/data

FROM gcr.io/distroless/static-debian12@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/sentinel /sentinel
COPY --from=build --chown=65532:65532 /out/data /data
ENV SENTINEL_DATA_DIR=/data
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=5 CMD ["/sentinel", "healthcheck"]
ENTRYPOINT ["/sentinel"]
