"""Regenerates the SQLite, font, certificate, key and mail samples of the file viewers inside the tools container, which has fontTools,
openssl and ssh-keygen:

    docker compose -f compose.dev.yaml run --rm tools python3 testenv/preview-samples/generate-misc.py

Everything is made here, so no third-party license applies: the font is drawn by this script, the certificates and keys are fresh and
signed by a throwaway authority, and the private key sample exists only to check that the viewer never shows it.
"""
import datetime
import os
import sqlite3
import subprocess
import tempfile
from email.message import EmailMessage
from email.utils import format_datetime, make_msgid

from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = {name: os.path.join(HERE, name) for name in ("sqlite", "fonts", "certificates", "mail")}
IMAGE = os.path.join(HERE, "images", "sample.png")


def write_sqlite(path):
    """Writes a database with tables, a view, an index, NULLs, blobs, text in several scripts and a table of 20 000 rows."""
    if os.path.exists(path):
        os.remove(path)
    db = sqlite3.connect(path)
    db.executescript(
        """
        CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT NOT NULL, city TEXT, amount REAL, joined TEXT, photo BLOB);
        CREATE TABLE events (id INTEGER PRIMARY KEY, person INTEGER REFERENCES people(id), kind TEXT, at TEXT);
        CREATE INDEX events_by_person ON events(person);
        CREATE VIEW totals AS SELECT city, COUNT(*) AS people, SUM(amount) AS amount FROM people GROUP BY city;
        CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT) WITHOUT ROWID;
        """
    )
    with open(IMAGE, "rb") as f:
        photo = f.read()
    people = [
        (1, "Ana", "Santiago", 1200.5, "2026-09-02", photo),
        (2, "Bruno", "Lima", 980.0, "2026-09-03", None),
        (3, "Carla", "Bogotá", 1530.25, "2026-09-04", None),
        (4, "Diego", None, None, None, None),
        (5, "Émilie 日本語 😀", "Montréal", 42.0, "2026-09-05", b"\x00\x01\x02"),
    ]
    db.executemany("INSERT INTO people VALUES (?, ?, ?, ?, ?, ?)", people)
    kinds = ("upload", "download", "delete", "rename")
    db.executemany(
        "INSERT INTO events (person, kind, at) VALUES (?, ?, ?)",
        [((i % 5) + 1, kinds[i % 4], (datetime.datetime(2026, 9, 1) + datetime.timedelta(minutes=i)).isoformat()) for i in range(20_000)],
    )
    db.executemany("INSERT INTO settings VALUES (?, ?)", [("theme", "dark"), ("language", "es"), ("<script>", "<img src=x onerror=alert(1)>")])
    db.commit()
    db.close()
    with open(path, "rb") as f:
        whole = f.read()
    with open(os.path.join(os.path.dirname(path), "truncated.sqlite"), "wb") as f:
        f.write(whole[:4096])


def glyph(width, strokes):
    """Draws a glyph of rectangles, each a tuple of x, y, width and height in font units."""
    pen = TTGlyphPen(None)
    for x, y, w, h in strokes:
        pen.moveTo((x, y))
        pen.lineTo((x, y + h))
        pen.lineTo((x + w, y + h))
        pen.lineTo((x + w, y))
        pen.closePath()
    return pen.glyph(), width


def write_fonts(folder):
    """Writes a small block font drawn here, as TrueType, WOFF and WOFF2, and a truncated one."""
    shapes = {
        "S": [(100, 0, 400, 100), (400, 0, 100, 400), (100, 300, 400, 100), (100, 300, 100, 400), (100, 600, 400, 100)],
        "E": [(100, 0, 100, 700), (100, 0, 400, 100), (100, 300, 300, 100), (100, 600, 400, 100)],
        "N": [(100, 0, 100, 700), (400, 0, 100, 700), (200, 450, 70, 150), (270, 300, 70, 150), (340, 150, 60, 150)],
        "T": [(250, 0, 100, 700), (100, 600, 400, 100)],
        "I": [(250, 0, 100, 700)],
        "L": [(100, 0, 100, 700), (100, 0, 400, 100)],
        "A": [(100, 0, 100, 700), (400, 0, 100, 700), (100, 300, 400, 100), (100, 600, 400, 100)],
        "O": [(100, 0, 100, 700), (400, 0, 100, 700), (100, 0, 400, 100), (100, 600, 400, 100)],
    }
    order = [".notdef", "space"] + [f"glyph{c}" for c in shapes]
    glyphs = {".notdef": glyph(600, [(50, 0, 500, 50), (50, 650, 500, 50), (50, 0, 50, 700), (500, 0, 50, 700)]), "space": glyph(300, [])}
    for char, strokes in shapes.items():
        glyphs[f"glyph{char}"] = glyph(600, strokes)
    builder = FontBuilder(1000, isTTF=True)
    builder.setupGlyphOrder(order)
    cmap = {ord(" "): "space"}
    for char in shapes:
        cmap[ord(char)] = f"glyph{char}"
        cmap[ord(char.lower())] = f"glyph{char}"
    builder.setupCharacterMap(cmap)
    builder.setupGlyf({name: shape for name, (shape, _) in glyphs.items()})
    builder.setupHorizontalMetrics({name: (width, 0) for name, (_, width) in glyphs.items()})
    builder.setupHorizontalHeader(ascent=800, descent=-200)
    builder.setupNameTable({"familyName": "Transferly Blocks", "styleName": "Regular", "version": "Version 1.0", "copyright": "Drawn for the Transferly preview samples"})
    builder.setupOS2(sTypoAscender=800, usWinAscent=800, usWinDescent=200)
    builder.setupPost()
    ttf = os.path.join(folder, "blocks.ttf")
    builder.save(ttf)
    for flavor in ("woff", "woff2"):
        builder.font.flavor = flavor
        builder.save(os.path.join(folder, f"blocks.{flavor}"))
    with open(ttf, "rb") as f:
        whole = f.read()
    with open(os.path.join(folder, "truncated.ttf"), "wb") as f:
        f.write(whole[:200])


def openssl(*args, cwd):
    """Runs openssl quietly."""
    subprocess.run(["openssl", *args], check=True, cwd=cwd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def write_certificates(folder):
    """Writes a chain signed by a throwaway authority, an expired certificate, an elliptic curve one, an Ed25519 one with unusual names and
    usages, a DSA one, a certificate request, DER copies, public keys and a private key."""
    with tempfile.TemporaryDirectory() as work:
        openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", "ca.key", "-out", "ca.pem", "-days", "3650", "-subj", "/C=CL/O=Transferly Samples/CN=Transferly Sample Root CA", cwd=work)
        openssl("req", "-newkey", "rsa:2048", "-nodes", "-keyout", "leaf.key", "-out", "leaf.csr", "-subj", "/C=CL/ST=RM/L=Santiago/O=Transferly Samples/CN=files.example.com", cwd=work)
        with open(os.path.join(work, "leaf.ext"), "w") as f:
            f.write("subjectAltName=DNS:files.example.com,DNS:*.files.example.com,IP:192.0.2.10\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n")
        openssl("x509", "-req", "-in", "leaf.csr", "-CA", "ca.pem", "-CAkey", "ca.key", "-CAcreateserial", "-out", "leaf.pem", "-days", "825", "-extfile", "leaf.ext", cwd=work)
        openssl("req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes", "-keyout", "ec.key", "-out", "ec.pem", "-days", "365", "-subj", "/CN=ec.example.com", cwd=work)
        openssl("req", "-newkey", "rsa:2048", "-nodes", "-keyout", "old.key", "-out", "old.csr", "-subj", "/CN=expired.example.com", cwd=work)
        os.makedirs(os.path.join(work, "issued"))
        open(os.path.join(work, "index.txt"), "w").close()
        with open(os.path.join(work, "ca.cnf"), "w") as f:
            f.write("[ca]\ndefault_ca=d\n[d]\ndir=.\ndatabase=index.txt\nnew_certs_dir=issued\nserial=ca.srl\ndefault_md=sha256\npolicy=p\n[p]\ncommonName=supplied\n")
        openssl("ca", "-batch", "-config", "ca.cnf", "-cert", "ca.pem", "-keyfile", "ca.key", "-in", "old.csr", "-out", "old.pem", "-notext", "-startdate", "20200101000000Z", "-enddate", "20210101000000Z", cwd=work)
        with open(os.path.join(work, "unusual.ext"), "w") as f:
            f.write("subjectAltName=email:ops@example.com,URI:https://example.com/id,IP:2001:db8::1,dirName:dir\nextendedKeyUsage=1.2.3.4\n[dir]\nCN=Directory Name\n")
        openssl("req", "-newkey", "ed25519", "-nodes", "-keyout", "unusual.key", "-out", "unusual.csr", "-subj", "/organizationIdentifier=VATCL-12345678/CN=unusual.example.com", cwd=work)
        openssl("x509", "-req", "-in", "unusual.csr", "-CA", "ca.pem", "-CAkey", "ca.key", "-CAcreateserial", "-out", "unusual.pem", "-days", "30", "-extfile", "unusual.ext", cwd=work)
        openssl("dsaparam", "-out", "dsa.param", "2048", cwd=work)
        openssl("req", "-x509", "-newkey", "dsa:dsa.param", "-nodes", "-keyout", "dsa.key", "-out", "dsa.pem", "-days", "30", "-subj", "/O=DSA Samples", "-sha256", cwd=work)
        read = lambda name: open(os.path.join(work, name), "rb").read()
        for name in ("unusual.pem", "dsa.pem", "leaf.csr"):
            with open(os.path.join(folder, name), "wb") as f:
                f.write(read(name))
        with open(os.path.join(folder, "chain.pem"), "wb") as f:
            f.write(read("leaf.pem") + read("ca.pem"))
        with open(os.path.join(folder, "ec.crt"), "wb") as f:
            f.write(read("ec.pem"))
        with open(os.path.join(folder, "expired.pem"), "wb") as f:
            f.write(read("old.pem"))
        openssl("x509", "-in", "leaf.pem", "-outform", "DER", "-out", os.path.join(folder, "leaf.cer"), cwd=work)
        openssl("pkey", "-in", "ec.key", "-pubout", "-out", os.path.join(folder, "public-key.pem"), cwd=work)
        with open(os.path.join(folder, "private-key.pem"), "wb") as f:
            f.write(read("leaf.key"))
        subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "sample@transferly", "-f", os.path.join(work, "id")], check=True)
        with open(os.path.join(folder, "id_ed25519.pub"), "wb") as f:
            f.write(read("id.pub"))
    with open(os.path.join(folder, "broken.pem"), "w") as f:
        f.write("-----BEGIN CERTIFICATE-----\nnot base64 at all !!\n-----END CERTIFICATE-----\n")


def write_mail(folder):
    """Writes a newsletter with text, HTML, an inline picture and attachments, and a hostile message."""
    message = EmailMessage()
    message["From"] = "Transferly Samples <samples@example.com>"
    message["To"] = "Ana <ana@example.com>, bruno@example.com"
    message["Cc"] = "Carla <carla@example.com>"
    message["Subject"] = "Informe de septiembre · 📊 resultados"
    message["Date"] = format_datetime(datetime.datetime(2026, 9, 30, 12, 0, tzinfo=datetime.timezone.utc))
    message["Message-ID"] = make_msgid(domain="example.com")
    message.set_content("Hola Ana,\n\nAdjunto el informe de septiembre.\n\nSaludos,\nTransferly\n")
    cid = make_msgid(domain="example.com")
    message.add_alternative(
        f"<html><body style='font-family:sans-serif'><h1>Informe de septiembre</h1><p>Hola <b>Ana</b>,</p>"
        f"<p>Adjunto el informe de septiembre. El logo va incrustado:</p><img src='cid:{cid[1:-1]}' width='320' alt='logo'>"
        f"<table border='1' cellpadding='4'><tr><th>Ciudad</th><th>Monto</th></tr><tr><td>Santiago</td><td>1.200,50</td></tr></table></body></html>",
        subtype="html",
    )
    with open(IMAGE, "rb") as f:
        picture = f.read()
    message.get_payload()[1].add_related(picture, maintype="image", subtype="png", cid=cid)
    with open(os.path.join(HERE, "text", "plain.txt"), "rb") as f:
        message.add_attachment(f.read(), maintype="text", subtype="plain", filename="notas.txt")
    message.add_attachment(picture, maintype="image", subtype="png", filename="grafico.png")
    with open(os.path.join(folder, "newsletter.eml"), "wb") as f:
        f.write(bytes(message))

    hostile = EmailMessage()
    hostile["From"] = "attacker@example.com"
    hostile["To"] = "victim@example.com"
    hostile["Subject"] = "<img src=x onerror=alert(1)> urgent"
    hostile.set_content("See the HTML part.")
    hostile.add_alternative(
        "<html><head><style>body{background:url(https://example.com/css.png)}</style><script>alert('mail')</script></head>"
        "<body onload=\"alert('onload')\"><p>Tracking pixel:</p><img src='https://example.com/pixel.gif?id=victim'>"
        "<a href='javascript:alert(1)'>click me</a><form action='https://example.com/steal'><input name='password'></form>"
        "<iframe src='https://example.com/frame'></iframe></body></html>",
        subtype="html",
    )
    with open(os.path.join(folder, "hostile.eml"), "wb") as f:
        f.write(bytes(hostile))


def main():
    """Writes every sample of this group."""
    for folder in OUT.values():
        os.makedirs(folder, exist_ok=True)
    write_sqlite(os.path.join(OUT["sqlite"], "sample.sqlite"))
    write_fonts(OUT["fonts"])
    write_certificates(OUT["certificates"])
    write_mail(OUT["mail"])


if __name__ == "__main__":
    main()
