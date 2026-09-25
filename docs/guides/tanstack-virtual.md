# TanStack Virtual

Source: [Virtual React](https://tanstack.com/virtual/latest/docs/framework/react/react-virtual). Package: `@tanstack/react-virtual`.

## Setup

`useVirtualizer({ count, getScrollElement, estimateSize, overscan })`, a scroll container with a fixed height and `overflow: auto`, an inner element of `getTotalSize()` height and the items positioned with `translateY(item.start)`. Rows have a fixed height (32 px), so no measuring is needed.

## Rules

- Long lists (directory listings, transfer history) are virtualized from 100 rows; short ones render plainly.
- Keep keys stable (the entry path), not the index.
- Keyboard navigation moves a roving focus and calls `scrollToIndex`, since rows outside the window do not exist in the DOM.
- The row count is announced with `aria-rowcount` and each row carries `aria-rowindex`.

## Tests

jsdom has no layout, so `src/test/setup.ts` makes every element report a 900 by 400 box (`offsetWidth` and `offsetHeight`, which is what the virtualizer reads): the virtualizer then draws a known window and tests assert which rows exist. Components take no test-only props.
