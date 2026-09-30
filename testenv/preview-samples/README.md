# Preview samples

One sample per format the file viewers of Sentinel show (ADR-0018), and hostile samples that must be refused or shown safely. The folder is mounted read-only into the SFTP server of the test stacks as `/preview-samples`, so every viewer can be tried in the development stack and is opened by the end-to-end test `preview.spec.ts`.

| Folder | What | Viewer |
|---|---|---|
| `binary/` | random bytes, an empty file, binary bytes named `.txt` | hexadecimal (D2.0) |
| `text/` | plain UTF-8 text, Go and Python code, an 800-line log | text with highlighting (D2.1) |
| `markdown/` | a rich README, and hostile Markdown (scripts, handlers, `javascript:` links, frames, forms, remote images) that must neither run nor load | Markdown in the isolated page (D2.1) |
| `data/` | JSON, JSON Lines, YAML with anchors, a YAML alias bomb, XML, broken JSON, CSV with 2000 rows, TSV, CSV with semicolons | tree and table (D2.1) |

Each stage of D2 adds its samples here. They are generated for Sentinel, so no third-party license applies.
