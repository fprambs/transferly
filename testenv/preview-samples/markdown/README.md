# Sentinel preview sample

Markdown is **rendered** and *sanitized* inside an isolated page, with `inline code`, [a link](https://example.com) and lists:

- one
- two
  - nested

1. first
2. second

> A quote, as a note from the team.

| Format | Viewer | Stage |
|---|---|---|
| Markdown | rendered | D2.1 |
| CSV | table | D2.1 |
| PDF | pdf.js | D2.4 |

```go
func main() { fmt.Println("code block") }
```

---

An inline image as a data URI: ![dot](data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAYAAACNMs+9AAAAFUlEQVR42mNk+M9Qz0AEYBxVSF+FABJADq0/fZbCAAAAAElFTkSuQmCC)
