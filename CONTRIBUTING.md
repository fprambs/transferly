# Contributing

Everything runs in Docker; the host needs Docker with Compose v2 and git.

## Workflow

- Work in small units and commit each one in order.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): `feat`, `fix`, `docs`, `build`, `ci`, `test`, `refactor`, `perf`, `chore`, with an optional scope, for example `feat(backend): add site store`.
- Subjects are English, imperative and at most 72 characters.
- Enable the hooks once: `make hooks`.

## Definition of done

See the quality policy in [docs/ROADMAP.md](docs/ROADMAP.md): tests with every change, 100 percent coverage of production code, English documentation on every declaration, and no comments inside function bodies. Library conventions live in [docs/guides](docs/guides/README.md).

## Common commands

| Command | Purpose |
|---|---|
| `make up` | Start the development stack |
| `make test` | Run backend and frontend tests with coverage |
| `make lint` | Run linters and type checks |
| `make shell` | Open a shell in the tools container |
