# Resizable panels: react-resizable-panels

Sources (read 2026-09-28): the README and type definitions of [react-resizable-panels](https://github.com/bvaughn/react-resizable-panels) 4.9.0 (`npm pack`), the documentation routes of its repository (`src/routes`: platform requirements, collapsible, conditional and fixed-size panels, persistent layouts), and its separator source for keyboard handling. MIT, by Brian Vaughn; React 18 or newer. Used for 4.10.

## Model (version 4)

- `Group` (horizontal by default, `orientation="vertical"`) holds `Panel`s and `Separator`s, which must be its **direct DOM children**. It owns `display`, `flex-direction`, `flex-wrap` and `overflow`; they cannot be overridden.
- `Panel` sizes accept numbers (pixels), unitless strings or `"%"` strings (percentages of the group), and `px`, `em`, `rem`, `vh`, `vw`: `defaultSize`, `minSize`, `maxSize`, `collapsedSize`. A panel's `className` and `style` go to an inner `div`, so the panel itself keeps its flex layout; content that scrolls needs `min-h-0`/`overflow` on that inner element.
- `collapsible` panels collapse once dragged below half their `minSize` (`collapsedThreshold` to change it), to `collapsedSize` (0 by default). A collapsed panel reopens by dragging its separator or with `panelRef.expand()`. `defaultSize={0}` starts collapsed.
- `groupResizeBehavior="preserve-pixel-size"` keeps a panel's pixels when the window changes (side bars); at least one panel of the group must keep the default `preserve-relative-size`.
- Imperative handles through `panelRef` (`collapse`, `expand`, `isCollapsed`, `getSize` → `{ asPercentage, inPixels }`, `resize`) and `groupRef` (`getLayout`, `setLayout`). For panels that are rendered conditionally, use `usePanelCallbackRef`, not `usePanelRef` (issue #717).
- `onResize(size, id, previous)` on a panel runs on every change; `onLayoutChanged` on a group runs once the pointer is released and is the one to persist with (`onLayoutChange` runs on every pointer move).

## Separators and accessibility

- `role="separator"` with `aria-controls`, `aria-orientation`, `aria-valuemin/max/now` rendered by the library; extra attributes such as `aria-label` pass through, so every separator gets a name.
- Keyboard: arrows resize, Home/End go to the limits, **Enter** collapses or expands a collapsible panel, **F6** (Shift+F6) moves between the separators of the group.
- Double click resets the panel next to it to its `defaultSize`, unless `disableDoubleClick`.
- State for styling: `data-separator` is `inactive`, `hover`, `active`, or `disabled`. Hit area: `resizeTargetMinimumSize` on the group (defaults are fine for a mouse; the visible line stays 1 px and a wider transparent area catches the pointer).
- The library sets drag cursors itself (`disableCursor` to opt out).

## Persistence

`useDefaultLayout({ id, storage })` returns `{ defaultLayout, onLayoutChanged }` to spread on the group; `storage` is anything with `getItem`/`setItem` (`localStorage` by default). A `Layout` maps panel ids to their flex-grow, so every panel needs a stable `id`. Groups with conditionally rendered panels pass `panelIds` so each set of panels keeps its own layout. Sentinel wraps storage in try/catch, since `localStorage` can throw in private windows, and treats layouts as a per-browser convenience.

## Tests

- It needs `ResizeObserver`; jsdom has none, so the Vitest setup installs a no-op one. jsdom measures nothing, so unit tests check structure, names, keyboard handlers wiring and the show/hide switches, not pixel sizes.
- Playwright (real layout) checks dragging a separator, the size surviving a reload, double click restoring the default, and keyboard resizing.

## Rules in Sentinel

- One `ResizeHandle` component draws every separator (a 1 px line that highlights on hover and while dragging) with an accessible name.
- Panels that can be hidden (sites, log, details, transfers) are collapsible and follow the interface preferences: the toolbar switches and the panels' own toggles collapse and expand them, and dragging one closed updates the preference.
- Group ids start with `sentinel-` and are the storage keys; changing the panels of a group means changing its id, so an old saved layout does not apply to new panels.
