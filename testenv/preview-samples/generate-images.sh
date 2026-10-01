#!/bin/sh
# Regenerates the image samples of the file viewers with ImageMagick, inside the tools container:
#   docker compose -f compose.dev.yaml run --rm tools sh testenv/preview-samples/generate-images.sh
set -eu
cd "$(dirname "$0")/images"

base() {
  label="$1"
  output="$2"
  shift 2
  convert -size 640x400 gradient:'#0f766e'-'#1e3a8a' \
    -fill white -font DejaVu-Sans-Bold -pointsize 56 -gravity center -annotate +0-20 'Transferly' \
    -pointsize 22 -annotate +0+40 "$label" base.png
  convert base.png "$@" "$output"
  rm base.png
}

base 'PNG with transparency' sample.png -alpha set -channel A -evaluate set 90% +channel
base 'JPEG with EXIF' sample-noexif.jpg
base 'WebP' sample.webp
base 'AVIF' sample.avif
base 'BMP' BMP3:sample.bmp
base 'TIFF' sample.tiff -compress lzw
convert -size 64x64 xc:'#0f766e' -fill white -font DejaVu-Sans-Bold -pointsize 40 -gravity center -annotate +0+0 'T' \
  -define icon:auto-resize=64,32,16 sample.ico
for frame in 1:'#0f766e' 2:'#1e3a8a' 3:'#9d174d'; do
  convert -size 200x120 "xc:${frame#*:}" -fill white -font DejaVu-Sans-Bold -pointsize 48 -gravity center -annotate +0+0 "${frame%%:*}" "frame-${frame%%:*}.gif"
done
convert -delay 40 -loop 0 frame-1.gif frame-2.gif frame-3.gif animated.gif
rm frame-1.gif frame-2.gif frame-3.gif
head -c 600 sample.png > truncated.png

python3 - <<'PY'
import struct

def entry(tag, kind, count, value):
    return struct.pack('<HHI', tag, kind, count) + value

def ascii_value(text, data, offset):
    raw = text.encode() + b'\0'
    if len(raw) <= 4:
        return raw.ljust(4, b'\0'), data
    return struct.pack('<I', offset + len(data)), data + raw

def ifd(entries, start, next_ifd=0):
    data = b''
    base = start + 2 + 12 * len(entries) + 4
    body = b''
    for tag, kind, text in entries:
        if kind == 2:
            value, data = ascii_value(text, data, base)
            body += entry(tag, 2, len(text) + 1, value)
        else:
            body += entry(tag, kind, 1, struct.pack('<I', text))
    return struct.pack('<H', len(entries)) + body + struct.pack('<I', next_ifd) + data

exif_entries = [(0x9003, 2, '2026:09:29 18:30:00'), (0x8827, 3, 200)]
first = [(0x010F, 2, 'Transferly Camera'), (0x0110, 2, 'Preview One'), (0x0131, 2, 'Transferly samples')]
first_size = len(ifd(first + [(0x8769, 4, 0)], 8))
exif_ifd = ifd(exif_entries, 8 + first_size)
tiff = b'II*\0' + struct.pack('<I', 8) + ifd(first + [(0x8769, 4, 8 + first_size)], 8) + exif_ifd
app1 = b'Exif\0\0' + tiff
segment = b'\xff\xe1' + struct.pack('>H', len(app1) + 2) + app1
jpeg = open('sample-noexif.jpg', 'rb').read()
open('sample.jpg', 'wb').write(jpeg[:2] + segment + jpeg[2:])
PY
rm sample-noexif.jpg
