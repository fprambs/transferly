# The SFTP server of the test stacks that reports sizes it does not send. Build it from the root of the repository.
ARG GO_VERSION=1.27
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
COPY backend/third_party/ ./third_party/
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/strictsftp ./cmd/strictsftp

FROM gcr.io/distroless/static-debian12@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/strictsftp /strictsftp
USER 65532:65532
EXPOSE 22
ENTRYPOINT ["/strictsftp"]
