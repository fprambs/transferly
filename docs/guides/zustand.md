# Zustand

Source: [Beginner TypeScript guide](https://zustand.docs.pmnd.rs/learn/guides/beginner-typescript).

Zustand holds client UI state (open panels, selection, active paths, preferences). Server data belongs to TanStack Query.

```ts
export const usePanelStore = create<PanelState>()((set) => ({
  path: '/',
  setPath: (path) => set({ path }),
}));
```

## Rules

1. Use the curried form `create<T>()(...)`.
2. Always use selectors; never subscribe to the whole store.
3. Select several fields with `useShallow`.
4. Derive values in selectors; do not store duplicates.
5. Keep actions inside the store.
6. One store per UI domain.

## Tests

Use `setState` and `getState`; reset stores between tests.
