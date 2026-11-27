# Rebuild the official Caddy release with the security dependency updates.
FROM golang:1.27.4-trixie@sha256:430985542100b871e108ccc00e8dcc5eef08ca14ee5825e98ebf563d8ffec4ee AS build
ENV GOTOOLCHAIN=local
WORKDIR /src
# Build Caddy as a dependency so its version is recorded in the binary.
COPY <<'GO' main.go
package main

import (
	_ "time/tzdata"

	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
)

func main() {
	caddycmd.Main()
}
GO

# Caddy #8368 replaces the Smallstep revision that depended on go-jose v3.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go mod init transferly-caddy-build \
    && go get github.com/caddyserver/caddy/v2/cmd/caddy@v2.11.9 \
        github.com/smallstep/certificates@v0.30.3-0.20261105213751-6d5a251fbe01 \
        github.com/go-jose/go-jose/v4@v4.1.9 \
        github.com/klauspost/compress@v1.20.3 \
        golang.org/x/net@v0.62.0 \
    && CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/caddy .

FROM caddy:2.11.9@sha256:4e2425f6009577e7c651010f16679c28f7d12df3e7b1914f941a7a3370746b3c
COPY --from=build /out/caddy /usr/bin/caddy
RUN setcap cap_net_bind_service=+ep /usr/bin/caddy
