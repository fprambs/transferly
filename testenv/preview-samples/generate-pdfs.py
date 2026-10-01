"""Regenerates the PDF samples of the file viewers with the standard library only, inside the tools container:

    docker compose -f compose.dev.yaml run --rm tools python3 testenv/preview-samples/generate-pdfs.py
"""
import hashlib
import os

HERE = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'pdf')
PAD = bytes.fromhex('28bf4e5e4e758a4164004e56fffa01082e2e00b6d0683e802f0ca9fe6453697a')


def rc4(key, data):
    """Encrypts or decrypts data with RC4, the cipher of the standard security handler of revision 2."""
    box = list(range(256))
    j = 0
    for i in range(256):
        j = (j + box[i] + key[i % len(key)]) % 256
        box[i], box[j] = box[j], box[i]
    out = bytearray()
    i = j = 0
    for byte in data:
        i = (i + 1) % 256
        j = (j + box[i]) % 256
        box[i], box[j] = box[j], box[i]
        out.append(byte ^ box[(box[i] + box[j]) % 256])
    return bytes(out)


def escape(text):
    """Escapes text for a literal string of a content stream."""
    return text.replace('\\', '\\\\').replace('(', '\\(').replace(')', '\\)')


def page_content(number, lines):
    """The content stream of a page: a title and lines of text in Helvetica."""
    parts = ['BT /F1 22 Tf 72 740 Td (%s) Tj ET' % escape('Transferly manual - page %d' % number)]
    y = 700
    for line in lines:
        parts.append('BT /F1 12 Tf 72 %d Td (%s) Tj ET' % (y, escape(line)))
        y -= 18
    return '\n'.join(parts).encode('latin-1')


def build(path, contents, extra_catalog='', encrypt_password=None):
    """Writes a PDF whose page objects come first and whose content streams come last, so a viewer reading by ranges needs only the
    start and the end of the file to lay out every page."""
    count = len(contents)
    objects = {}
    file_id = hashlib.md5(path.encode()).digest()
    key = None
    encrypt_ref = ''
    if encrypt_password is not None:
        user = (encrypt_password.encode() + PAD)[:32]
        owner_key = hashlib.md5(user).digest()[:5]
        owner_entry = rc4(owner_key, user)
        permissions = -44
        key = hashlib.md5(user + owner_entry + permissions.to_bytes(4, 'little', signed=True) + file_id).digest()[:5]
        user_entry = rc4(key, PAD)
    first_page = 4
    first_content = first_page + count
    objects[1] = '<< /Type /Catalog /Pages 2 0 R %s>>' % extra_catalog
    kids = ' '.join('%d 0 R' % (first_page + i) for i in range(count))
    objects[2] = '<< /Type /Pages /Kids [%s] /Count %d >>' % (kids, count)
    objects[3] = '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>'
    for i in range(count):
        objects[first_page + i] = '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>' % (first_content + i)
    streams = {first_content + i: data for i, data in enumerate(contents)}
    total = first_content + count
    if key is not None:
        objects[total] = '<< /Filter /Standard /V 1 /R 2 /O <%s> /U <%s> /P %d >>' % (owner_entry.hex(), user_entry.hex(), permissions)
        encrypt_ref = '/Encrypt %d 0 R ' % total
        total += 1
    out = bytearray(b'%PDF-1.4\n%\xe2\xe3\xcf\xd3\n')
    offsets = {}
    for number in range(1, total):
        offsets[number] = len(out)
        if number in streams:
            data = streams[number]
            if key is not None:
                data = rc4(hashlib.md5(key + number.to_bytes(3, 'little') + b'\0\0').digest()[:10], data)
            out += b'%d 0 obj\n<< /Length %d >>\nstream\n' % (number, len(data)) + data + b'\nendstream\nendobj\n'
        else:
            out += ('%d 0 obj\n%s\nendobj\n' % (number, objects[number])).encode('latin-1')
    xref = len(out)
    out += b'xref\n0 %d\n0000000000 65535 f \n' % total
    for number in range(1, total):
        out += b'%010d 00000 n \n' % offsets[number]
    out += ('trailer\n<< /Size %d /Root 1 0 R %s/ID [<%s> <%s>] >>\nstartxref\n%d\n%%%%EOF\n' % (total, encrypt_ref, file_id.hex(), file_id.hex(), xref)).encode('latin-1')
    with open(os.path.join(HERE, path), 'wb') as target:
        target.write(out)
    return bytes(out)


os.makedirs(HERE, exist_ok=True)
manual = []
for number in range(1, 31):
    lines = ['This is page %d of the manual of the Transferly file viewer.' % number, 'Each page is drawn when it comes into view.']
    if number == 17:
        lines.append('The rare word quetzal appears only on this page.')
    manual.append(page_content(number, lines))
build('manual.pdf', manual)

filler = ['Line %03d of a long page that makes this document large on purpose.' % line for line in range(36)]
large = [page_content(number, filler) + b'\n%' + b'x' * 6000 for number in range(1, 401)]
build('large.pdf', large)

build('with-javascript.pdf', [page_content(1, ['This PDF asks to run JavaScript when it opens.', 'The viewer never runs it.'])],
      extra_catalog="/OpenAction << /S /JavaScript /JS (app.alert\\('owned'\\)) >> /Names << /JavaScript << /Names [(owned) << /S /JavaScript /JS (app.alert\\(1\\)) >>] >> >> ")
build('protected.pdf', [page_content(1, ['A document protected with the password transferly.'])], encrypt_password='transferly')
whole = build('broken-source.pdf', [page_content(1, ['This file is cut short.'])])
with open(os.path.join(HERE, 'broken.pdf'), 'wb') as target:
    target.write(whole[:300])
os.remove(os.path.join(HERE, 'broken-source.pdf'))
