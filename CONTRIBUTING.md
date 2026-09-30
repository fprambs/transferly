# Contributing

Everything runs in Docker; the host needs Docker with Compose v2 and git.

## License

By contributing you agree that your work is licensed under the [Apache License 2.0](LICENSE).

## Workflow

- Work in small units and commit each one in order.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): `feat`, `fix`, `docs`, `build`, `ci`, `test`, `refactor`, `perf`, `chore`, with an optional scope, for example `feat(backend): add site store`.
- Subjects are English, imperative and at most 72 characters.
- Enable the hooks once: `make hooks`.

## Definition of done

See the quality policy in [docs/ROADMAP.md](docs/ROADMAP.md): tests with every change, 100 percent coverage of production code, English documentation on every declaration, and no comments inside function bodies. Library conventions live in [docs/guides](docs/guides/README.md).

## Translations

To add or improve a language of the interface, follow [Adding a language](docs/guides/i18n.md#adding-a-language): copy the English folder, translate it, register the language in one line and run the translation tests.

## Common commands

| Command | Purpose |
|---|---|
| `make up` | Start the development stack |
| `make test` | Run backend and frontend tests with coverage |
| `make lint` | Run linters and type checks |
| `make gen` | Regenerate code from the API contract and SQL |
| `make shell` | Open a shell in the tools container |
