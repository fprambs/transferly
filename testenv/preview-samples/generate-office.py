"""Regenerates the Office samples of the file viewers inside the tools container, which has python-docx, python-pptx, XlsxWriter, xlwt and
odfpy:

    docker compose -f compose.dev.yaml run --rm tools python3 testenv/preview-samples/generate-office.py

The documents are written by the same libraries real programs use, so Word, Excel, PowerPoint and LibreOffice open them. The hostile
document is a real one whose parts are then edited to carry what a viewer must never run or load.
"""
import datetime
import io
import os
import zipfile

import xlsxwriter
import xlwt
from docx import Document
from docx.enum.text import WD_BREAK
from docx.opc.constants import RELATIONSHIP_TYPE
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Inches, Pt, RGBColor
from odf.draw import Frame, Image as OdfImage
from odf.opendocument import OpenDocumentSpreadsheet, OpenDocumentText
from odf.style import ParagraphProperties, Style, TableColumnProperties, TextProperties
from odf.table import Table, TableCell, TableColumn, TableRow
from odf.text import H, List, ListItem, P, Span
from pptx import Presentation
from pptx.dml.color import RGBColor as PptxColor
from pptx.util import Inches as PptxInches, Pt as PptxPt

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "office")
IMAGE = os.path.join(HERE, "images", "sample.png")
PEOPLE = [("Ana", "Santiago", 1200.5), ("Bruno", "Lima", 980.0), ("Carla", "Bogotá", 1530.25), ("Diego", "Quito", 760.75)]


def hyperlink(paragraph, url, text):
    """Adds an external hyperlink to a paragraph, which python-docx has no call for."""
    rel = paragraph.part.relate_to(url, RELATIONSHIP_TYPE.HYPERLINK, is_external=True)
    link = OxmlElement("w:hyperlink")
    link.set(qn("r:id"), rel)
    run = OxmlElement("w:r")
    props = OxmlElement("w:rPr")
    style = OxmlElement("w:rStyle")
    style.set(qn("w:val"), "Hyperlink")
    props.append(style)
    run.append(props)
    content = OxmlElement("w:t")
    content.text = text
    run.append(content)
    link.append(run)
    paragraph._p.append(link)


def write_docx(path):
    """Writes a Word document with headings, styled text, lists, a table, an image, a link, a header, a footer and two pages."""
    doc = Document()
    doc.sections[0].header.paragraphs[0].text = "Transferly · preview samples"
    doc.sections[0].footer.paragraphs[0].text = "Generated for the file viewers of Transferly"
    doc.add_heading("Transferly preview sample", level=0)
    doc.add_heading("A Word document", level=1)
    intro = doc.add_paragraph("This document has ")
    intro.add_run("bold").bold = True
    intro.add_run(", ")
    intro.add_run("italic").italic = True
    intro.add_run(", ")
    intro.add_run("underlined").underline = True
    intro.add_run(" and ")
    colored = intro.add_run("colored")
    colored.font.color.rgb = RGBColor(0x0E, 0x9F, 0x6E)
    intro.add_run(" text, in several sizes: ")
    intro.add_run("large").font.size = Pt(18)
    intro.add_run(".")
    doc.add_heading("Lists", level=2)
    for item in ("Quick Look style viewer", "Files read by ranges", "Risky parsers in an isolated page"):
        doc.add_paragraph(item, style="List Bullet")
    for item in ("Select a file", "Press Space", "Walk the folder with the arrows"):
        doc.add_paragraph(item, style="List Number")
    doc.add_heading("A table", level=2)
    table = doc.add_table(rows=1, cols=3)
    table.style = "Light Grid Accent 1"
    for cell, title in zip(table.rows[0].cells, ("Name", "City", "Amount")):
        cell.text = title
    for name, city, amount in PEOPLE:
        cells = table.add_row().cells
        cells[0].text, cells[1].text, cells[2].text = name, city, f"{amount:,.2f}"
    doc.add_heading("An image and a link", level=2)
    doc.add_picture(IMAGE, width=Inches(3))
    hyperlink(doc.add_paragraph("Project page: "), "https://example.com/transferly", "example.com/transferly")
    doc.add_paragraph().add_run().add_break(WD_BREAK.PAGE)
    doc.add_heading("Second page", level=1)
    doc.add_paragraph("The viewer lays the document out page by page, as Word does. " * 8)
    doc.save(path)


def write_hostile_docx(path):
    """Writes a Word document that links to javascript:, loads an image from the network and embeds HTML with a script and handlers."""
    doc = Document()
    doc.add_heading("Hostile document", level=1)
    hyperlink(doc.add_paragraph("A link that must not run: "), "javascript:alert(document.domain)", "click me")
    doc.add_paragraph("A remote image that must not load, and embedded HTML that must not render:")
    buffer = io.BytesIO()
    doc.save(buffer)
    with zipfile.ZipFile(io.BytesIO(buffer.getvalue())) as source, zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as target:
        for item in source.infolist():
            data = source.read(item.filename)
            if item.filename == "word/document.xml":
                chunk = b'<w:altChunk r:id="rIdHostileHtml"/>'
                remote = (
                    b'<w:p><w:r><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">'
                    b'<wp:extent cx="952500" cy="952500"/><wp:docPr id="99" name="remote"/>'
                    b'<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">'
                    b'<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:nvPicPr><pic:cNvPr id="99" name="remote"/><pic:cNvPicPr/></pic:nvPicPr>'
                    b'<pic:blipFill><a:blip r:link="rIdRemoteImage"/></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="952500" cy="952500"/></a:xfrm>'
                    b'<a:prstGeom prst="rect"/></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>'
                )
                data = data.replace(b"<w:sectPr", remote + chunk + b"<w:sectPr", 1)
            if item.filename == "word/_rels/document.xml.rels":
                data = data.replace(
                    b"</Relationships>",
                    b'<Relationship Id="rIdHostileHtml" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/aFChunk" Target="hostile.html"/>'
                    b'<Relationship Id="rIdRemoteImage" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" '
                    b'Target="https://example.com/tracker.png" TargetMode="External"/></Relationships>',
                )
            if item.filename == "[Content_Types].xml":
                data = data.replace(b"</Types>", b'<Default Extension="html" ContentType="text/html"/></Types>')
            target.writestr(item, data)
        target.writestr("word/hostile.html", b'<html><body><script>alert("altChunk")</script><img src=x onerror="alert(1)"><p>embedded html</p></body></html>')


def write_odt(path):
    """Writes an OpenDocument text with headings, styled spans, a list, a table and an image."""
    doc = OpenDocumentText()
    bold = Style(name="Bold", family="text")
    bold.addElement(TextProperties(fontweight="bold"))
    italic = Style(name="Italic", family="text")
    italic.addElement(TextProperties(fontstyle="italic"))
    colored = Style(name="Colored", family="text")
    colored.addElement(TextProperties(color="#0e9f6e"))
    centered = Style(name="Centered", family="paragraph")
    centered.addElement(ParagraphProperties(textalign="center"))
    for style in (bold, italic, colored, centered):
        doc.automaticstyles.addElement(style)
    doc.text.addElement(H(outlinelevel=1, text="Transferly preview sample"))
    doc.text.addElement(H(outlinelevel=2, text="An OpenDocument text"))
    paragraph = P(text="This document has ")
    paragraph.addElement(Span(stylename=bold, text="bold"))
    paragraph.addText(", ")
    paragraph.addElement(Span(stylename=italic, text="italic"))
    paragraph.addText(" and ")
    paragraph.addElement(Span(stylename=colored, text="colored"))
    paragraph.addText(" text.")
    doc.text.addElement(paragraph)
    doc.text.addElement(P(stylename=centered, text="A centered paragraph."))
    items = List()
    for text in ("Written by odfpy", "Read by Transferly in the browser", "Never sent anywhere"):
        item = ListItem()
        item.addElement(P(text=text))
        items.addElement(item)
    doc.text.addElement(items)
    doc.text.addElement(H(outlinelevel=2, text="A table"))
    table = Table(name="People")
    table.addElement(TableColumn(numbercolumnsrepeated=3))
    for row in (("Name", "City", "Amount"), *[(n, c, f"{a:,.2f}") for n, c, a in PEOPLE]):
        tr = TableRow()
        for value in row:
            cell = TableCell()
            cell.addElement(P(text=value))
            tr.addElement(cell)
        table.addElement(tr)
    doc.text.addElement(table)
    href = doc.addPicture(IMAGE)
    frame = Frame(width="8cm", height="5cm", anchortype="as-char", name="sample")
    frame.addElement(OdfImage(href=href))
    holder = P()
    holder.addElement(frame)
    doc.text.addElement(holder)
    doc.save(path)


def write_xlsx(path):
    """Writes an Excel workbook with styled headers, numbers, dates, formulas with their results, merged cells and a sheet of 5000 rows."""
    book = xlsxwriter.Workbook(path)
    header = book.add_format({"bold": True, "bg_color": "#0e9f6e", "font_color": "#ffffff"})
    money = book.add_format({"num_format": "#,##0.00"})
    date = book.add_format({"num_format": "yyyy-mm-dd"})
    sales = book.add_worksheet("Sales")
    sales.merge_range("A1:D1", "Sales by person", header)
    for column, title in enumerate(("Name", "City", "Amount", "Date")):
        sales.write(1, column, title, header)
    for row, (name, city, amount) in enumerate(PEOPLE, start=2):
        sales.write(row, 0, name)
        sales.write(row, 1, city)
        sales.write_number(row, 2, amount, money)
        sales.write_datetime(row, 3, datetime.datetime(2026, 9, row), date)
    total = sum(amount for _, _, amount in PEOPLE)
    sales.write(len(PEOPLE) + 2, 1, "Total", header)
    sales.write_formula(len(PEOPLE) + 2, 2, f"=SUM(C3:C{len(PEOPLE) + 2})", money, total)
    sales.freeze_panes(2, 0)
    sales.set_column("A:D", 14)
    notes = book.add_worksheet("Notes")
    notes.write("A1", "Formulas show their saved results; the viewer never computes or runs anything.")
    notes.write_formula("A3", '=HYPERLINK("javascript:alert(1)","a formula link")', None, "a formula link")
    big = book.add_worksheet("Big")
    for column, title in enumerate(("Row", "Value", "Square")):
        big.write(0, column, title, header)
    for row in range(1, 5001):
        big.write_number(row, 0, row)
        big.write_number(row, 1, row * 1.5)
        big.write_formula(row, 2, f"=A{row + 1}*A{row + 1}", None, row * row)
    book.close()


def write_xls(path):
    """Writes a legacy Excel 97 workbook, the binary format before xlsx."""
    book = xlwt.Workbook()
    sheet = book.add_sheet("Legacy")
    bold = xlwt.easyxf("font: bold on")
    for column, title in enumerate(("Name", "City", "Amount")):
        sheet.write(0, column, title, bold)
    for row, (name, city, amount) in enumerate(PEOPLE, start=1):
        sheet.write(row, 0, name)
        sheet.write(row, 1, city)
        sheet.write(row, 2, amount)
    book.save(path)


def write_ods(path):
    """Writes an OpenDocument spreadsheet with two sheets."""
    doc = OpenDocumentSpreadsheet()
    wide = Style(name="Wide", family="table-column")
    wide.addElement(TableColumnProperties(columnwidth="3cm"))
    doc.automaticstyles.addElement(wide)
    for name, rows in (("People", [("Name", "City", "Amount"), *PEOPLE]), ("Cities", [("City",), *[(c,) for _, c, _ in PEOPLE]])):
        table = Table(name=name)
        table.addElement(TableColumn(stylename=wide, numbercolumnsrepeated=len(rows[0])))
        for row in rows:
            tr = TableRow()
            for value in row:
                if isinstance(value, float):
                    cell = TableCell(valuetype="float", value=value)
                else:
                    cell = TableCell(valuetype="string")
                cell.addElement(P(text=str(value)))
                tr.addElement(cell)
            table.addElement(tr)
        doc.spreadsheet.addElement(table)
    doc.save(path)


def write_pptx(path):
    """Writes a PowerPoint deck: a title slide, a slide of bullets, one with an image and shapes, and one with a table."""
    deck = Presentation()
    deck.slide_width, deck.slide_height = PptxInches(13.333), PptxInches(7.5)
    title = deck.slides.add_slide(deck.slide_layouts[0])
    title.shapes.title.text = "Transferly preview sample"
    title.placeholders[1].text = "A PowerPoint deck read in the browser"
    bullets = deck.slides.add_slide(deck.slide_layouts[1])
    bullets.shapes.title.text = "What the viewer shows"
    frame = bullets.placeholders[1].text_frame
    frame.text = "Slides as they are laid out"
    for text, level in (("Text boxes with their sizes and colors", 1), ("Pictures", 1), ("Simple tables", 1), ("Speaker notes are left out", 0)):
        paragraph = frame.add_paragraph()
        paragraph.text, paragraph.level = text, level
    picture = deck.slides.add_slide(deck.slide_layouts[5])
    picture.shapes.title.text = "A picture and shapes"
    picture.shapes.add_picture(IMAGE, PptxInches(0.8), PptxInches(1.8), width=PptxInches(6))
    box = picture.shapes.add_shape(1, PptxInches(7.5), PptxInches(2), PptxInches(4.5), PptxInches(1.5))
    box.fill.solid()
    box.fill.fore_color.rgb = PptxColor(0x0E, 0x9F, 0x6E)
    box.text_frame.text = "A filled shape"
    box.text_frame.paragraphs[0].runs[0].font.size = PptxPt(28)
    box.text_frame.paragraphs[0].runs[0].font.bold = True
    note = picture.shapes.add_textbox(PptxInches(7.5), PptxInches(4), PptxInches(4.5), PptxInches(2))
    note.text_frame.text = "A plain text box, in italics."
    note.text_frame.paragraphs[0].runs[0].font.italic = True
    tabular = deck.slides.add_slide(deck.slide_layouts[5])
    tabular.shapes.title.text = "A table"
    table = tabular.shapes.add_table(len(PEOPLE) + 1, 3, PptxInches(1), PptxInches(2), PptxInches(11), PptxInches(3)).table
    for column, text in enumerate(("Name", "City", "Amount")):
        table.cell(0, column).text = text
    for row, (name, city, amount) in enumerate(PEOPLE, start=1):
        table.cell(row, 0).text, table.cell(row, 1).text, table.cell(row, 2).text = name, city, f"{amount:,.2f}"
    deck.save(path)


def write_broken():
    """Writes a legacy Word 97 file, which falls back to bytes, and a truncated docx, which must fail safely."""
    with open(os.path.join(OUT, "legacy.doc"), "wb") as f:
        f.write(bytes.fromhex("d0cf11e0a1b11ae1") + bytes(4088))
    with open(os.path.join(OUT, "document.docx"), "rb") as f:
        whole = f.read()
    with open(os.path.join(OUT, "truncated.docx"), "wb") as f:
        f.write(whole[: len(whole) // 2])


def main():
    """Writes every Office sample."""
    os.makedirs(OUT, exist_ok=True)
    write_docx(os.path.join(OUT, "document.docx"))
    write_hostile_docx(os.path.join(OUT, "hostile.docx"))
    write_odt(os.path.join(OUT, "document.odt"))
    write_xlsx(os.path.join(OUT, "workbook.xlsx"))
    write_xls(os.path.join(OUT, "legacy.xls"))
    write_ods(os.path.join(OUT, "sheet.ods"))
    write_pptx(os.path.join(OUT, "slides.pptx"))
    write_broken()


if __name__ == "__main__":
    main()
