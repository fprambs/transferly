# Preview samples

One sample per format the file viewers of Sentinel show (ADR-0018), and hostile samples that must be refused or shown safely. The folder is mounted read-only into the SFTP server of the test stacks as `/preview-samples`, so every viewer can be tried in the development stack and is opened by the end-to-end test `preview.spec.ts`.

| Folder | What | Viewer |
|---|---|---|
| `binary/` | random bytes, an empty file, binary bytes named `.txt` | hexadecimal (D2.0) |
| `text/` | plain UTF-8 text | text (D2.1; shown as bytes until then) |

Each stage of D2 adds its samples here. They are generated for Sentinel, so no third-party license applies.
