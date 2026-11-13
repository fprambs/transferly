# The fake of Google of the test stack (ADR-0024). Build it from the root of the repository.
ARG GO_VERSION=1.27
FROM golang:${GO_VERSION}-trixie AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
COPY backend/third_party/ ./third_party/
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/fakegoogle ./cmd/fakegoogle

FROM gcr.io/distroless/static-debian13@sha256:1a64f46364620465ef9e9a8f5413757dfdf22695e050bbfd7303eeaf17a59383
COPY --from=build /out/fakegoogle /fakegoogle
USER 65532:65532
EXPOSE 8090
ENTRYPOINT ["/fakegoogle"]
