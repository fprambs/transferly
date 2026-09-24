# Documentation

Sources: [Go Doc Comments](https://go.dev/doc/comment), [TSDoc](https://tsdoc.org/), [ESLint no-inline-comments](https://eslint.org/docs/latest/rules/no-inline-comments).

## Rules

- Development artifacts are English: identifiers, comments, errors, tests, guides, ADRs, commits. `docs/ARCHITECTURE.md`, `docs/ROADMAP.md` and `docs/security/` are Spanish.
- Terse and standard: one sentence when one is enough.
- Document everything: packages, types, functions and methods, exported or not, tests and test helpers included.
- Document declarations, not lines: no comments inside function bodies. Express intent with names.
- Directives are allowed (`//go:embed`, `//nolint`).
- Non-Go files (Dockerfile, YAML, SQL) keep only short header or section comments, in English.

## Go

- Every package has a doc comment starting with `Package name ...`; commands start with the program name.
- Every declaration has a doc comment starting with its name, in full sentences.
- Functions say what they return or do; concurrency safety and zero values are stated when not obvious.
- Link identifiers with `[Name]` or `[pkg.Name]`; deprecations use a `Deprecated:` paragraph.
- Test functions start with their name (`TestX verifies that ...`) and list the use cases they cover.
- `gofmt` formats doc comments.

```go
// TestLoad_Errors verifies that Load rejects invalid configuration and names each offending variable.
//
// Scenarios:
//   - missing database URL
//   - unknown log level
func TestLoad_Errors(t *testing.T)
```

## TypeScript and React

- TSDoc `/** ... */` on every declaration: components, hooks, functions, types, constants and test files' helpers and suites.
- Do not repeat types; document behavior with `@param`, `@returns`, `@throws`, `@defaultValue`, `@example`, `@remarks`, `@deprecated`.
- Components document their purpose; each prop is documented on the props interface.
- Test suites (`describe`) and cases (`it`) are named after the use case; a TSDoc block above each suite lists its scenarios.
- No inline comments; no comments inside function bodies.

```tsx
/** Lists the entries of a remote directory. */
export interface FileListProps {
  /** Absolute remote path to display. */
  path: string;
}
```

## Enforcement

| Check | Where |
|---|---|
| Package and exported symbol docs | `revive` rules `package-comments`, `exported` |
| Every package, function, method and type documented, name first, tests included | `backend/internal/docscheck` |
| No comments inside function bodies (Go) | `backend/internal/docscheck` |
| Every declaration, member and test suite documented; no comments inside function bodies (TypeScript) | `frontend/guards` (Vitest and the TypeScript compiler API), tested by `docs-rules.test.ts` |
| No inline comments (TypeScript) | oxlint `no-inline-comments` |
| Terse docs, scenarios listed | code review |
