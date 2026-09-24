# React

Source: [You Might Not Need an Effect](https://react.dev/learn/you-might-not-need-an-effect).

An Effect synchronizes with an external system. Without one, do not use an Effect.

## Rules

1. Derive data during render, or with `useMemo` when expensive; never mirror it in state through an Effect.
2. Handle user actions in event handlers.
3. Reset state on prop change with `key`.
4. Store ids and derive objects instead of syncing state from props.
5. Do not chain Effects; compute in the handler.
6. Do not notify parents from Effects; update both in the same handler or lift state.
7. Subscribe to external stores with `useSyncExternalStore`.
8. Fetch data with TanStack Query, not manual Effects.

Legitimate Effects: connect and disconnect an external resource (global key listeners, observers), always with cleanup.

## Tests

Testing Library: assert visible behavior by role and text, not implementation.
