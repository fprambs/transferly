# ADR-0018: File preview inside Sentinel

- Status: accepted
- Date: 2026-09-29

## Context

Sentinel lists, downloads and edits remote files, but seeing what a file holds means downloading it. The differentiator D2, moved before phase 5, asks for a full suite of viewers inside the platform: text and code, Markdown, structured data, tables, images, audio and video, PDF, archives, Office documents, SQLite databases, fonts, certificates and mail, for remote files, local files and files inside archives. Rendering content a remote server controls is the most dangerous surface of the product: a crafted PDF, document or SVG must not reach the session of the person looking at it.

## Decision

**One full-screen viewer, in the style of Quick Look.** Space or a double click opens the selected file, the arrows move to the previous and next file of the folder, Escape closes. The bar shows the name, size and type, and offers download, open in the editor for text, and the controls of the viewer in use. It works the same for remote files, the files of the local panel and, later, the entries of an archive, because every viewer reads through one source interface: the size, and reads of a byte range.

**Nothing is read whole when a range is enough.** A remote file is read with `Range` requests to the existing content endpoint, which already serves single ranges; a local file with `File.slice`. The type is told from the first 4 KiB (magic numbers) before the extension, so a file with a lying extension shows as what it is. Each viewer has a size limit; past it, the hexadecimal viewer, which reads any file page by page, is always available.

**Formats with complex parsers run in an isolated page.** `/sandbox/viewer.html` is served with its own headers: a Content-Security-Policy with the `sandbox allow-scripts` directive, so the page has an opaque origin even when opened directly, `connect-src 'none'` and no other network, blobs only for images, media, fonts and workers, and framing allowed only by Sentinel itself. The explorer embeds it in an `iframe` with `sandbox="allow-scripts"`, without `allow-same-origin`: the page has no cookies, cannot call the API and cannot reach the parent. The parent sends it the bytes of the file by `postMessage` and accepts only messages whose source is that frame; a renderer that needs more than the first bytes, such as PDF.js, asks the parent for ranges by message, and the page forwards Escape and the arrows, so the viewer keeps hearing them while the focus is inside it. The built assets are public and are served with `Access-Control-Allow-Origin: *` and `Cross-Origin-Resource-Policy: cross-origin`, since the opaque page loads them as another origin. PDF, rendered Markdown, Office documents, SQLite databases, fonts and mail bodies are viewed there; text, tables, images (SVG as an image, which runs no script), audio, video, archive trees and the hexadecimal view are drawn by the main page, as text or by the browser's own decoders.

**Archives are read by the server**, in Go, without fetching the whole file: a ZIP by its central directory and the ranges of the entry asked for, a TAR stream until the entry, with caps on entries, uncompressed bytes and compression ratio. An entry is a source like any other, so it opens with the same viewers. Archives of the local panel never leave the computer: the browser reads ZIP, TAR and gzip itself, with its native `DecompressionStream` and the same caps, and leaves the other formats to the server.

**Office documents are rendered in the browser**, inside the isolated page, by JavaScript libraries; no conversion service is run. Fidelity is good for Word and Excel and fair for PowerPoint; legacy `.doc` and `.ppt` fall back to the hexadecimal view.

**Previewing is downloading.** It needs the download right, so a viewer can preview, and goes through the same endpoints and limits as a download.

**Every viewer can be tried.** A folder `preview-samples/`, with at least one sample per format and hostile samples (decompression bombs, hostile paths, an SVG with a script, a PDF with JavaScript, a huge CSV, truncated and mislabeled files), is seeded into the SFTP server of the test stack, and an end-to-end test opens each one.

## Consequences

- Positive: files are seen without leaving the platform; large files start showing at once; a flaw in a parser stays inside a page with no session, no API and no network.
- Negative: the isolated page needs its own headers and public assets; the libraries of the heavy viewers (PDF, Office, SQLite) grow the build and are loaded only when a file needs them; formats the browser cannot decode (some video codecs, legacy Office) fall back to download or the hexadecimal view.
