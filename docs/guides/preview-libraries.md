# Libraries of the file viewers

Sources: the README and type definitions of each package: [marked](https://marked.js.org/), [DOMPurify](https://github.com/cure53/DOMPurify), [yaml](https://eemeli.org/yaml/), [Papa Parse](https://www.papaparse.com/docs). Architecture in [ADR-0018](../adr/0018-file-preview.md).

## marked and DOMPurify (Markdown)

- marked does not sanitize its output; its own README requires a sanitizer on the output HTML. Always `DOMPurify.sanitize(marked.parse(text))`, never `marked.parse` alone.
- Both run only inside the isolated page (`/sandbox/viewer.html`), never in the main page: even a sanitizer bypass lands in a page with no cookies, no API and no network.
- Sanitize with `USE_PROFILES: { html: true }` (no SVG or MathML), and forbid `style` and `form` elements.
- Links never navigate: the page cancels their clicks. Remote images do not load, since the policy of the page allows only `blob:` and `data:` images.
- DOMPurify needs a real browser DOM; do not run it on server-side DOM shims such as happy-dom, which its README warns are unsafe. Unit tests of the Markdown renderer run in jsdom, which DOMPurify supports.

## yaml (YAML)

- `YAML.parse(text)` keeps the default `maxAliasCount` of 100, which stops exponential alias expansion (the "billion laughs" attack); never set it to `-1`.
- Parse errors are shown with their message and the source view; the tree only shows documents that parsed.

## Papa Parse (CSV and TSV)

- `Papa.parse(text, { skipEmptyLines: true })` guesses the delimiter among comma, tab, pipe and semicolon; `.tsv` files force the tab.
- Parse in the main thread only up to the size limit of the table viewer (20 MiB); the rows are drawn virtually, so the limit is the parse, not the drawing.
- Cells are always drawn as text.
