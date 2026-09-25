# Forms: react-hook-form and zod

Sources: [react-hook-form](https://react-hook-form.com/get-started), [@hookform/resolvers](https://github.com/react-hook-form/resolvers), [zod](https://zod.dev).

## Setup

- `useForm({ resolver: zodResolver(schema) })` with the schema next to the form. Types come from the schema: `z.infer` (or `z.input` and `z.output` when it has defaults or transforms: `useForm<z.input<S>, unknown, z.output<S>>`).
- Register inputs with `register('name')`; numbers use `valueAsNumber`; show `errors.name?.message`.
- Uncontrolled inputs by default; use `Controller` only for components that are not native inputs.

## Rules

- Messages in the schema are i18n keys, not text: the form translates them (`t(($) => $.errors[key])`), so validation follows the language.
- Every input has a `<label htmlFor>`, an `aria-invalid` when it has an error and `aria-describedby` for its message; the first invalid field takes focus (`shouldFocusError`, the default).
- The schema mirrors the server rules that are cheap to check (lengths, formats) but the server stays the authority: its `problem.code` is mapped to a field error or a form error.
- Secrets (passwords, keys) are never put in `defaultValues` from stored data and are cleared after submit.

## Tests

Type into the form with `userEvent`, submit, and assert the calls to the API (MSW) and the messages by role and label.
