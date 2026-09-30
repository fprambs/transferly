# Libraries of the file viewers

Sources: the README and type definitions of each package: [marked](https://marked.js.org/), [DOMPurify](https://github.com/cure53/DOMPurify), [yaml](https://eemeli.org/yaml/), [Papa Parse](https://www.papaparse.com/docs), [UTIF.js](https://github.com/photopea/UTIF.js), [exifr](https://github.com/MikeKovarik/exifr), [PDF.js](https://mozilla.github.io/pdf.js/) (`pdfjs-dist` 6), and for archives, read by the backend in Go, the package documentation of [archive/zip and archive/tar](https://pkg.go.dev/archive), [bodgit/sevenzip](https://pkg.go.dev/github.com/bodgit/sevenzip), [nwaples/rardecode](https://pkg.go.dev/github.com/nwaples/rardecode/v2), [ulikunitz/xz](https://pkg.go.dev/github.com/ulikunitz/xz) and [klauspost/compress/zstd](https://pkg.go.dev/github.com/klauspost/compress/zstd). Architecture in [ADR-0018](../adr/0018-file-preview.md).

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

## Images

- The browser decodes every format it knows (PNG, JPEG, GIF, WebP, AVIF, BMP, ICO) from a `blob:` URL, which the policy of the page allows for images only; the URL is revoked when the viewer closes.
- SVG is shown as an `<img>` with the `image/svg+xml` type: an image never runs the scripts, handlers or links of the SVG, and loads nothing from the network.
- TIFF, which browsers do not draw, is decoded by UTIF.js (`UTIF.decode`, `UTIF.decodeImage` on the first page, `UTIF.toRGBA8`) onto a canvas; images over 100 megapixels are refused before decoding, so a crafted header cannot ask for gigabytes of pixels. UTIF.js ships no types; `src/types/utif.d.ts` declares the three functions used.
- EXIF is read with the `lite` bundle of exifr, the one its README recommends for browsers (IFD0, EXIF and GPS), keeping only the tags the viewer shows; its `pick` option fails in the lite bundle, so it is not used. A file whose metadata cannot be read simply shows none.

## PDF.js (PDF)

- Runs only inside the isolated page. Its worker is bundled into the page (`pdfjs-dist/build/pdf.worker.min.mjs?worker&inline`) and started from a `blob:` URL through `GlobalWorkerOptions.workerPort`, since the page cannot load scripts from the network.
- The file is read with a `PDFDataRangeTransport`: the page gets the first MiB and the size, and pdf.js asks for the rest in chunks of 64 KiB (`rangeChunkSize`, `disableAutoFetch`), which the page asks the parent for by message. The parent reads them from the file, at most 4 MiB at a time.
- Options that would need the network stay off: `useWasm: false`, `useSystemFonts: true` (no standard font data or CMaps are fetched, so some CJK documents may show with fallback fonts), `enableXfa: false`. The display layer of PDF.js 6 never runs the JavaScript of a document.
- A `PasswordException` is reported as a protected document; the viewer does not ask for passwords.
- The renderer is loaded with a dynamic import only when a PDF opens, so the Markdown viewer does not carry it.


## Archives (Go, backend)

- The package `internal/core/archives` reads archives without writing anything to disk. Entry names are data: they are listed as the archive stores them (`../`, absolute and Windows paths included) and never joined to a path of the server.
- ZIP (`archive/zip`) and 7z (`sevenzip.NewReader` over an `io.ReaderAt`) are read by offset, so a remote archive is listed from its index at the end without fetching the rest. Encrypted ZIP entries (flag bit 0) are refused before reading. `sevenzip` reports a missing password as a `*sevenzip.ReadError` with `Encrypted` set, while opening an archive with encrypted headers or only once it decrypts an entry, so both the opening and the reads are mapped.
- TAR, compressed TARs and single compressed files are streams read from the start: gzip and bzip2 from the standard library, xz with `xz.ReaderConfig{DictCap: 64 MiB}` and zstd with `zstd.WithDecoderMaxWindow(64 MiB)` and a single goroutine, so a header cannot ask for gigabytes of memory. RAR uses `rardecode.NewReader` with `MaxDictionarySize(64 MiB)` (the default is 4 GiB); multi-volume RARs are not followed, since the reader has no file system. RAR cannot be created by free tools, so its samples are written by hand as RAR5 with the stored method.
- Caps: 10 000 entries per listing, 512 MiB per entry, 1 GiB decompressed while scanning a stream, and a declared ratio over 200 on entries over 1 MiB is taken for a bomb. An entry that yields more than it declared fails instead of being cut.
