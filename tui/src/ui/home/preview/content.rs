//! File type detection and parsing for previews.

use std::io::Cursor;

use calamine::{Reader, open_workbook_auto_from_rs};

use super::syntax::{Language, Token};
use crate::ui::widgets::visible_name;

/// The most rows read from each delimited file or worksheet, including its header.
const ROW_LIMIT: usize = 5_000;

/// How many records to compare to distinguish delimited data from prose.
const SAMPLE_ROWS: usize = 8;

/// The presentation chosen for the file.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum Kind {
    /// Plain text or source code.
    Text,
    /// JSON formatted as text.
    Json,
    /// Delimited records that use commas or semicolons.
    Csv,
    /// Delimited records that use tabs.
    Tsv,
    /// A workbook with one or more sheets.
    Workbook,
    /// An image to open in the web application.
    Image,
    /// A PDF to open in the web application.
    Pdf,
    /// Another binary file.
    Binary,
}

impl Kind {
    /// The translated name of the presentation.
    pub(super) fn label(self) -> &'static str {
        match self {
            Self::Text => "preview.text",
            Self::Json => "preview.json",
            Self::Csv => "preview.csv",
            Self::Tsv => "preview.tsv",
            Self::Workbook => "preview.workbook",
            Self::Image => "preview.image",
            Self::Pdf => "preview.pdf",
            Self::Binary => "preview.binary",
        }
    }
}

/// The data that can be shown inside a preview.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) enum Body {
    /// Displayable lines of text.
    Text(Text),
    /// A delimited file or the sheets of a workbook.
    Tables(Vec<Table>),
    /// A message offering to open the web application.
    Web,
}

/// Displayable text with an optional language and cached syntax transitions.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) struct Text {
    /// The decoded text, split into display lines.
    pub(super) lines: Vec<String>,
    /// The syntax name shown in the title.
    pub(super) language: Option<&'static str>,
    /// The syntax transitions of each highlighted line.
    pub(super) highlights: Vec<Vec<Token>>,
}

/// One delimited file or worksheet, with widths in terminal cells.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) struct Table {
    /// The workbook sheet name, empty for delimited files.
    pub(super) name: String,
    /// The header followed by its records.
    pub(super) rows: Vec<Vec<String>>,
    /// The widest field of each column.
    pub(super) widths: Vec<u16>,
}

impl Table {
    /// Measures all columns, including optional fields beyond the header.
    fn new(name: String, rows: Vec<Vec<String>>) -> Self {
        let mut widths = Vec::new();
        for row in &rows {
            widths.resize(widths.len().max(row.len()), 1);
            for (width, cell) in widths.iter_mut().zip(row) {
                let cells = ratatui::text::Span::raw(cell.as_str()).width();
                *width = (*width).max(u16::try_from(cells).unwrap_or(u16::MAX));
            }
        }
        Self { name, rows, widths }
    }
}

/// Chooses a preview by the extension when it is known and from the bytes otherwise.
pub(super) fn parse(name: &str, bytes: &[u8], cut: bool) -> Result<(Kind, Body), ()> {
    let extension = std::path::Path::new(name)
        .extension()
        .and_then(std::ffi::OsStr::to_str)
        .unwrap_or_default()
        .to_ascii_lowercase();
    match extension.as_str() {
        "csv" | "tsv" => {
            let preferred = if extension == "tsv" { b'\t' } else { b',' };
            let delimiter = detect_delimiter(bytes, Some(preferred)).unwrap_or(preferred);
            let kind = if delimiter == b'\t' {
                Kind::Tsv
            } else {
                Kind::Csv
            };
            Ok((kind, delimited(bytes, delimiter)))
        }
        "xlsx" | "xlsm" | "xls" | "ods" => match workbook(bytes) {
            Err(()) if cut => Ok((Kind::Workbook, Body::Web)),
            outcome => outcome,
        },
        "json" => {
            let json = pretty_json(bytes);
            Ok(json_preview(json.as_ref().map_or(bytes, String::as_bytes)))
        }
        "png" | "apng" | "jpg" | "jpeg" | "gif" | "bmp" | "tif" | "tiff" | "webp" | "ico"
        | "avif" | "heic" => Ok((Kind::Image, Body::Web)),
        "pdf" => Ok((Kind::Pdf, Body::Web)),
        "txt" => Ok(structured_text(bytes).unwrap_or_else(|| (Kind::Text, text(bytes, None)))),
        _ => Ok(
            if let Some(language) = Language::for_file(name, &extension) {
                (Kind::Text, text(bytes, Some(language)))
            } else {
                sniff(bytes)
            },
        ),
    }
}

/// Recognizes common file signatures and structured text without an extension.
fn sniff(bytes: &[u8]) -> (Kind, Body) {
    if bytes.starts_with(b"%PDF-") {
        return (Kind::Pdf, Body::Web);
    }
    if [
        b"\x89PNG\r\n\x1a\n".as_slice(),
        b"\xff\xd8\xff",
        b"GIF87a",
        b"GIF89a",
        b"BM",
        b"II*\0",
        b"MM\0*",
    ]
    .iter()
    .any(|magic| bytes.starts_with(magic))
        || (bytes.starts_with(b"RIFF") && bytes.get(8..12) == Some(b"WEBP".as_slice()))
    {
        return (Kind::Image, Body::Web);
    }
    if bytes.starts_with(b"PK\x03\x04") || bytes.starts_with(b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1") {
        return workbook(bytes).unwrap_or((Kind::Binary, Body::Web));
    }
    if bytes.iter().take(1024).any(|byte| *byte == 0) {
        return (Kind::Binary, Body::Web);
    }
    if let Some(preview) = structured_text(bytes) {
        return preview;
    }
    match detect_delimiter(bytes, None) {
        Some(b'\t') => (Kind::Tsv, delimited(bytes, b'\t')),
        Some(delimiter) => (Kind::Csv, delimited(bytes, delimiter)),
        None => (Kind::Text, text(bytes, None)),
    }
}

/// Detects JSON and other source formats whose first lines identify the language.
fn structured_text(bytes: &[u8]) -> Option<(Kind, Body)> {
    if let Some(json) = pretty_json(bytes) {
        return Some(json_preview(json.as_bytes()));
    }
    let decoded = String::from_utf8_lossy(without_bom(bytes));
    let language = Language::for_content(&decoded)?;
    Some((Kind::Text, text(bytes, Some(language))))
}

/// Builds a highlighted JSON preview from its display bytes.
fn json_preview(bytes: &[u8]) -> (Kind, Body) {
    (Kind::Json, text(bytes, Language::for_file("", "json")))
}

/// Strips a UTF-8 byte order mark, if present.
fn without_bom(bytes: &[u8]) -> &[u8] {
    bytes.strip_prefix(b"\xef\xbb\xbf").unwrap_or(bytes)
}

/// Decodes and indents a complete JSON value.
fn pretty_json(bytes: &[u8]) -> Option<String> {
    let value: serde_json::Value = serde_json::from_slice(without_bom(bytes)).ok()?;
    serde_json::to_string_pretty(&value).ok()
}

/// Decodes text lossily and makes control characters visible.
fn text(bytes: &[u8], language: Option<Language>) -> Body {
    let decoded = String::from_utf8_lossy(without_bom(bytes));
    let lines = decoded.lines().map(visible_name).collect();
    let highlights = language
        .as_ref()
        .map_or_else(Vec::new, |language| language.highlight(&decoded));
    Body::Text(Text {
        lines,
        language: language.map(|language| language.name),
        highlights,
    })
}

/// Reads loss-tolerant delimited records, with CSV quoting and variable row lengths.
fn records(bytes: &[u8], delimiter: u8) -> csv::Reader<&[u8]> {
    csv::ReaderBuilder::new()
        .delimiter(delimiter)
        .has_headers(false)
        .flexible(true)
        .from_reader(without_bom(bytes))
}

/// Finds the delimiter with the most consistently populated columns in the sample.
fn detect_delimiter(bytes: &[u8], preferred: Option<u8>) -> Option<u8> {
    (*b",;\t")
        .into_iter()
        .filter_map(|delimiter| {
            let mut reader = records(bytes, delimiter);
            let mut sample = reader.byte_records().take(SAMPLE_ROWS).flatten();
            let width = sample.next()?.len();
            let (rows, consistent) = sample.fold((1, 1), |(rows, consistent), row| {
                (rows + 1, consistent + usize::from(row.len() == width))
            });
            (width > 1 && (consistent > 1 || (rows == 1 && preferred.is_some())))
                .then_some(((consistent, width, Some(delimiter) == preferred), delimiter))
        })
        .max()
        .map(|(_, delimiter)| delimiter)
}

/// Builds the delimited file's table with its first record as the header.
fn delimited(bytes: &[u8], delimiter: u8) -> Body {
    let rows = records(bytes, delimiter)
        .byte_records()
        .take(ROW_LIMIT)
        .flatten()
        .map(|row| {
            row.iter()
                .map(|cell| visible_name(&String::from_utf8_lossy(cell)))
                .collect()
        })
        .collect();
    Body::Tables(vec![Table::new(String::new(), rows)])
}

/// Builds one table per sheet, using the cached values of formula cells.
fn workbook(bytes: &[u8]) -> Result<(Kind, Body), ()> {
    let mut workbook = open_workbook_auto_from_rs(Cursor::new(bytes)).map_err(workbook_error)?;
    let mut tables = Vec::new();
    for name in workbook.sheet_names() {
        let range = workbook.worksheet_range(&name).map_err(workbook_error)?;
        let rows = range
            .rows()
            .take(ROW_LIMIT)
            .map(|row| {
                row.iter()
                    .map(|cell| visible_name(&cell.to_string()))
                    .collect()
            })
            .collect();
        tables.push(Table::new(visible_name(&name), rows));
    }
    Ok((Kind::Workbook, Body::Tables(tables)))
}

/// Reduces a calamine error to a preview that could not be read.
#[allow(
    clippy::needless_pass_by_value,
    reason = "map_err passes the error by value"
)]
fn workbook_error(_: calamine::Error) {}

#[cfg(test)]
mod tests {
    use super::*;

    /// Known extensions take precedence, followed by signatures and structured text.
    #[test]
    fn recognizes_preview_kinds() {
        for (name, bytes, kind) in [
            ("log.txt", b"{\"data\":1}".as_slice(), Kind::Json),
            ("config.JSON", b"{\"data\":1}", Kind::Json),
            ("source.rs", b"{\"data\":1}", Kind::Text),
            (
                "script",
                b"#!/usr/bin/env python\nprint('hello')",
                Kind::Text,
            ),
            ("plain.txt", b"ordinary text", Kind::Text),
            ("notes", b"ordinary text", Kind::Text),
            ("report", b"%PDF-1.7", Kind::Pdf),
            ("photo", b"\xff\xd8\xff", Kind::Image),
            ("photo", b"RIFFxxxxWEBP", Kind::Image),
            ("photo", b"RIFFxxxxnope", Kind::Text),
            ("photo.png", b"", Kind::Image),
            ("raw", b"a\0b", Kind::Binary),
            ("archive", b"PK\x03\x04broken", Kind::Binary),
            ("data", b"a;b\nc;d", Kind::Csv),
            ("data", b"a\tb\nc\td", Kind::Tsv),
            ("data", b"{\"data\":1}", Kind::Json),
            ("empty.tsv", b"", Kind::Tsv),
            ("header.csv", b"a,b", Kind::Csv),
            ("header.csv", b"a;b", Kind::Csv),
        ] {
            let (found, _) = parse(name, bytes, false).expect("preview");
            assert_eq!(found, kind, "{name}: {bytes:?}");
            assert!(found.label().starts_with("preview."));
        }
    }

    /// UTF-8 decoding and JSON indentation keep invalid input readable.
    #[test]
    fn formats_text_and_json() {
        for (bytes, cut, expected) in [
            (
                b"\xef\xbb\xbf{\"name\":\"Ana\"}".as_slice(),
                false,
                vec!["{", "  \"name\": \"Ana\"", "}"],
            ),
            (b"\xff{\t".as_slice(), true, vec!["�{⇥"]),
        ] {
            let (kind, body) = parse("data.json", bytes, cut).expect("preview");
            assert_eq!(kind, Kind::Json);
            assert!(matches!(
                body,
                Body::Text(text) if text.lines == expected && text.language == Some("JSON")
            ));
        }
    }

    /// Delimited data honors every separator, CSV quoting and the row cap.
    #[test]
    #[rustfmt::skip]
    fn reads_delimited_records() {
        for delimiter in [',', ';', '\t'] {
            let header = format!("Name{delimiter}Value\n");
            let row = format!("\"a{delimiter}\"\"b\"{delimiter}\"line\nnext\"\r\n");
            let bytes = header + &row.repeat(ROW_LIMIT);
            let (_, Body::Tables(tables)) = parse("data.csv", bytes.as_bytes(), false).expect("preview") else { panic!("expected a table") };
            let table = &tables[0];
            assert_eq!(table.rows.len(), ROW_LIMIT);
            assert_eq!(table.rows[0], ["Name", "Value"]);
            assert_eq!(
                table.rows[1],
                [
                    visible_name(&format!("a{delimiter}\"b")),
                    "line�next".to_owned()
                ]
            );
            assert_eq!(table.widths, [4, 9]);
        }
    }

    /// Existing office fixtures exercise both Excel formats, ODS and content detection.
    #[test]
    #[rustfmt::skip]
    fn reads_workbook_formats() {
        let xlsx = include_bytes!("../../../../../testenv/preview-samples/office/workbook.xlsx");
        let xls = include_bytes!("../../../../../testenv/preview-samples/office/legacy.xls");
        let ods = include_bytes!("../../../../../testenv/preview-samples/office/sheet.ods");
        for (name, bytes) in [
            ("book.xlsx", xlsx.as_slice()),
            ("book.xlsm", xlsx.as_slice()),
            ("book.xls", xls.as_slice()),
            ("book.ods", ods.as_slice()),
            ("book", xlsx.as_slice()),
        ] {
            let (Kind::Workbook, Body::Tables(tables)) = parse(name, bytes, false).expect(name) else { panic!("expected a workbook") };
            assert_ne!(tables[0].name, "");
            assert!(tables.iter().all(|table| table.rows.len() <= ROW_LIMIT));
        }
        assert_eq!(parse("bad.xlsx", b"PK", false), Err(()));
        assert_eq!(
            parse("cut.xlsx", b"PK", true),
            Ok((Kind::Workbook, Body::Web))
        );
    }
}
