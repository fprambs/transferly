# DayPicker

Sentinel draws its calendars with [DayPicker](https://daypicker.dev) through `src/components/ui/calendar.tsx`. This guide sums up what the official documentation says and how the project follows it.

## Package

- Version 10 publishes under the name `@daypicker/react`; `react-day-picker` is the previous name, which v10 still re-exports. Install and import `@daypicker/react` only.
- Locales come from `@daypicker/react/locale` (date-fns locales): `es` and `enUS` today. `Calendar` picks one from the interface language.

## Styling

- The default `style.css` is not imported. Every part is styled with the `classNames` prop, whose keys are the `UI`, `DayFlag` and `SelectionState` names (`root`, `month_caption`, `nav`, `button_previous`, `day`, `day_button`, `selected`, `range_start`, `range_middle`, `range_end`, `today`, `outside`, `disabled`…).
- Colors come from the theme tokens (`primary`, `accent`, `muted-foreground`), so the calendar follows every theme.
- The arrows are replaced through `components={{ Chevron }}` with the lucide icons of the rest of the interface.

## Range mode

- `mode="range"` with `selected: { from, to }` and `onSelect(range | undefined)`.
- `resetOnSelect` makes a click after a finished range start a new one, which reads better than moving the nearest end.
- Clicking the start of a one-day range clears it (`onSelect(undefined)`), unless `required` is set.

## Dates and time zones

- Days are calendar dates. The filter stores them as `YYYY-MM-DD` and converts with `dateOfDay`/`dayOfDate` (`src/features/explorer/days.ts`), at the browser's midnight, as DayPicker shows them.
- The `today` prop is set from the account's time zone, so the marked day matches the dates the filter compares, which are read in that zone. The `timeZone` prop is not used: comparing strings of days avoids mixing zones.

## Navigation

- `captionLayout="dropdown"` with `startMonth` and `endMonth` offers month and year choices to reach distant dates quickly; the dropdowns are drawn with the project's own select.

## Accessibility

- DayPicker follows the ARIA date picker pattern: the days are a grid, arrows move by day and week, Page Up/Down by month (Shift for years), Home/End to the week bounds, Enter/Space select. Day buttons carry full date labels ("Monday, June 1st, 2020"), which tests use to pick days.
