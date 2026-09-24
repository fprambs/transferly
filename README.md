# Sentinel

Self-hosted web client for FTP, FTPS and SFTP: multi-user workspaces, an encrypted credential vault and concurrent, resumable transfers.

## Run

```sh
git clone <repository> sentinel && cd sentinel
docker compose up -d
```

The application answers on http://localhost:8080. Only Docker with Compose v2 and git are required.

## Develop

```sh
make up          # backend and frontend with hot reload, and Postgres
make test        # tests with coverage
make lint        # linters and type checks
```

Add `--profile testenv` (`make testenv`) for the SFTP, FTP and FTPS test servers; see [testenv/README.md](testenv/README.md).

## License

Apache License 2.0; see [LICENSE](LICENSE).

## Documentation

- [Architecture](docs/ARCHITECTURE.md) and [roadmap](docs/ROADMAP.md) (Spanish)
- [Decisions](docs/adr), [library guides](docs/guides/README.md), [security reviews](docs/security/README.md)
- [Contributing](CONTRIBUTING.md)
