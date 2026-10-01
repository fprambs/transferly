# Libraries of the file viewers

Sources: the README and type definitions of each package: [marked](https://marked.js.org/), [DOMPurify](https://github.com/cure53/DOMPurify), [yaml](https://eemeli.org/yaml/), [Papa Parse](https://www.papaparse.com/docs), [UTIF.js](https://github.com/photopea/UTIF.js), [exifr](https://github.com/MikeKovarik/exifr), [PDF.js](https://mozilla.github.io/pdf.js/) (`pdfjs-dist` 6), [docx-preview](https://github.com/VolodymyrBaydalka/docxjs), [SheetJS](https://docs.sheetjs.com/docs/api/parse-options), [JSZip](https://stuk.github.io/jszip/), [sql.js](https://github.com/sql-js/sql.js), [postal-mime](https://github.com/postalsys/postal-mime), [@noble/hashes](https://github.com/paulmillr/noble-hashes), and for archives, read by the backend in Go, the package documentation of [archive/zip and archive/tar](https://pkg.go.dev/archive), [bodgit/sevenzip](https://pkg.go.dev/github.com/bodgit/sevenzip), [nwaples/rardecode](https://pkg.go.dev/github.com/nwaples/rardecode/v2), [ulikunitz/xz](https://pkg.go.dev/github.com/ulikunitz/xz) and [klauspost/compress/zstd](https://pkg.go.dev/github.com/klauspost/compress/zstd). Architecture in [ADR-0018](../adr/0018-file-preview.md).

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
- A protected PDF asks for its password through `loadingTask.onPassword(updatePassword, reason)`: the page shows its own field and hands what is typed to `updatePassword`, and `PasswordResponses.INCORRECT_PASSWORD` shows that the password was wrong. The password is typed inside the isolated page and never leaves it: it is not sent to the parent, the server or anywhere else, and it is forgotten when the viewer closes.
- The renderer is loaded with a dynamic import only when a PDF opens, so the Markdown viewer does not carry it.


## Archives (Go, backend)

- The package `internal/core/archives` reads archives without writing anything to disk. Entry names are data: they are listed as the archive stores them (`../`, absolute and Windows paths included) and never joined to a path of the server.
- ZIP (`archive/zip`) and 7z (`sevenzip.NewReader` over an `io.ReaderAt`) are read by offset, so a remote archive is listed from its index at the end without fetching the rest. Encrypted ZIP entries (flag bit 0) are refused before reading. `sevenzip` reports a missing password as a `*sevenzip.ReadError` with `Encrypted` set, while opening an archive with encrypted headers or only once it decrypts an entry, so both the opening and the reads are mapped.
- TAR, compressed TARs and single compressed files are streams read from the start: gzip and bzip2 from the standard library, xz with `xz.ReaderConfig{DictCap: 64 MiB}` and zstd with `zstd.WithDecoderMaxWindow(64 MiB)` and a single goroutine, so a header cannot ask for gigabytes of memory. RAR uses `rardecode.NewReader` with `MaxDictionarySize(64 MiB)` (the default is 4 GiB); multi-volume RARs are not followed, since the reader has no file system. RAR cannot be created by free tools, so its samples are written by hand as RAR5 with the stored method.
- Caps: 10 000 entries per listing, 512 MiB per entry, 1 GiB decompressed while scanning a stream, and a declared ratio over 200 on entries over 1 MiB is taken for a bomb. An entry that yields more than it declared fails instead of being cut.

## Archives of the computer (browser)

- No library: ZIP (central directory, ZIP64 records and extra fields), TAR (ustar, GNU long names, pax `path`, base-256 sizes) and gzip are parsed in `features/preview/local-archive.ts`, and deflate and gzip are decompressed with the browser's `DecompressionStream` (`deflate-raw` for ZIP entries, `gzip` for `.gz` and `.tar.gz`), which every supported browser ships.
- The file is read by slices (`Blob.slice`) into a stream, never whole, and every read is capped as on the server: 10 000 entries, 64 MiB per entry shown, 1 GiB decompressed while scanning a TAR, and the 200:1 ratio for bombs. An entry that yields more than it declares fails.
- Encrypted ZIP entries and compression methods other than stored and deflate are refused with the server's guided codes; 7z, RAR, bzip2, xz and zstd archives of the computer point to the server.

## Office documents (docx-preview, SheetJS, JSZip)

- Everything runs inside the isolated page, loaded with a dynamic import only when a document opens; the explorer reads the whole file (up to 50 MiB) and hands it to the page.
- **docx-preview** (`renderAsync(data, body, styles, options)`) lays out Word documents page by page. `renderAltChunks: false` keeps the HTML a document may embed from being rendered in an `iframe`, and comments, changes and experimental features stay off. Pictures come from the package as `blob:` addresses; links to the network do not load under the policy of the page (`img-src blob: data:`), and the page cancels clicks on links. Bullets of the Symbol and Wingdings fonts, written as private characters, are replaced by their Unicode characters, since those fonts are rarely installed. docx-preview needs JSZip as a peer dependency.
- **SheetJS** is installed from its official CDN (`https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz`), as its documentation requires: the npm registry copy (0.18.5) is years old and has known vulnerabilities. It is read with `type: 'array'`, `dense: true`, `sheetRows: 10000`, `cellFormula: false`, `cellHTML: false`, `cellStyles: false` and `bookVBA: false`: nothing is computed or run, formulas show the results the file saved, and cells are drawn as text. At most 200 columns are drawn. A workbook protected with a password is reported as protected.
- **OpenDocument text** and **PowerPoint** are read by Transferly's own code from their XML, unzipped with JSZip: only known elements become HTML, text is always set as text, colors must be hexadecimal and styles come from an allowed list; slides are laid out in fractions of their size and container units (`cqw`), so they scale without script. Legacy `.doc` and `.ppt` show as bytes.

## SQLite (sql.js)

- Runs inside the isolated page in its **asm.js build** (`sql.js/dist/sql-asm.js`), not the WebAssembly one: it needs no `wasm-unsafe-eval` in the policy of the page, which stays as strict as before, at the cost of a larger chunk loaded only when a database opens. `src/types/sql-asm.d.ts` declares it with the types of `@types/sql.js`.
- The database is a copy in memory (`new SQL.Database(bytes)`), opened with `PRAGMA query_only = ON`, so a query can neither change the file nor the copy. Tables are read by pages of 200 rows with quoted names, and a query shows at most 1000 rows; its errors are shown as text.

## Fonts (FontFace)

- A font is loaded inside the isolated page with `new FontFace(name, buffer)` and `document.fonts.add`: the browser checks the font (Chromium with OTS) before using it, and a font it refuses is reported as unreadable.
- The names, number of glyphs and mapped characters are read by Transferly's own reader of the `name`, `maxp` and `cmap` tables of TrueType, OpenType and WOFF files (WOFF tables are inflated with `DecompressionStream`); WOFF2, compressed with Brotli, tells only its format.

## Certificates and keys (own DER reader, @noble/hashes)

- Read in the main page as data, never as markup, by a small strict DER reader (`der.ts`) and the certificate reader (`x509.ts`): PEM blocks, DER certificates, public keys, certificate requests and OpenSSH public keys. Fingerprints use `@noble/hashes` (audited, no dependencies), since `crypto.subtle` does not exist outside secure contexts.
- **Private keys are recognized by their PEM label and never decoded or shown**, and their files do not offer their source.

## Email (postal-mime, DOMPurify)

- postal-mime 3 reads the message in the main page (`PostalMime.parse(bytes, { attachmentEncoding: 'arraybuffer' })`); its HTML is not cleaned by it, as its documentation warns. Addresses, subject and date are drawn as text. Pictures shown by `cid:` references are put in place as data addresses (up to 5 MiB each).
- The HTML body is cleaned by DOMPurify inside the isolated page, without scripts, handlers, forms, frames, objects, meta or base elements, and drawn in a shadow root so its styles and those of the page never mix. A DOMPurify hook removes remote addresses of pictures and CSS, so a tracking picture is not even asked for, also in development where the page has no policy; the same hook cleans rendered Markdown.
- postal-mime is pinned to 3.0.0: the frontend enforces pnpm's minimum release age, and the newest release was too recent.
