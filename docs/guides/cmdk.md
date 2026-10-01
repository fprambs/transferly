# cmdk

Transferly's command palette (4.14c, `src/features/palette/`) is built on [cmdk](https://github.com/pacocoursey/cmdk). This guide sums up what the official README says and how the project follows it.

## Parts

- `Command` is the root (`[cmdk-root]`). It filters and ranks its items as the user types, keeps one item selected and moves the selection with the arrow keys; `loop` wraps from the last item to the first.
- `Command.Input` (`[cmdk-input]`) is the search field; `value`/`onValueChange` control it.
- `Command.List` (`[cmdk-list]`) holds groups and items; its height is animatable through `--cmdk-list-height`.
- `Command.Group` (`[cmdk-group]`) takes a `heading` (`[cmdk-group-heading]`). Groups without matches are hidden with an attribute, not unmounted.
- `Command.Item` (`[cmdk-item]`) runs `onSelect` on Enter or click. Its `value` is what the filter matches, inferred from its text when left out, and `keywords` add words that match without being shown. Items carry `data-selected` and `data-disabled`.
- `Command.Empty` (`[cmdk-empty]`) shows when nothing matches.

## Filtering

The default filter ranks items by a fuzzy score of the search against the value and keywords. Transferly keeps it and gives each item keywords in both languages where the words differ (host and user of a site, path of a bookmark), so a search for a host finds its site.

## Dialog

`Command.Dialog` composes Radix UI's Dialog. Transferly does not use it: the palette lives in the project's own Base UI `Dialog`, like every other dialog, so focus trapping, Escape and the overlay behave the same everywhere. The dialog opens with the main modifier and K (Ctrl on Windows and Linux, ⌘ on Apple systems) and from the button of the top bar.

## Styling

Every part is styled with Tailwind classes through its data attributes (`data-[selected=true]:bg-accent`, `**:[[cmdk-group-heading]]:…`), with the theme tokens, so the palette follows every theme.

## Accessibility

cmdk renders the input as a combobox that controls a listbox of options and announces the selected item through `aria-activedescendant`; the README says it is tested with VoiceOver. The palette gives the root a `label`, and keyboard hints sit in its footer.
