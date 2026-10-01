"""Builds the Transferly brand kit: logos, app icons, icons, tokens and the preview board.

The wordmark is set in Geist and converted to outlines, so the SVGs do not depend on any installed font.

    pip install fonttools brotli uharfbuzz
    python branding/scripts/build.py --fonts <dir with Geist-Bold.ttf, Geist-Medium.ttf and Geist-Regular.ttf>

Geist ships in the `geist` npm package (dist/fonts/geist-sans) under the SIL Open Font License.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path

import uharfbuzz as hb
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont

ROOT = Path(__file__).resolve().parent.parent

# Palette

PALETTE = {
    'core-blue': ('#0D47A1', 'Core Blue', 'Brand, links, primary actions on light surfaces'),
    'action-orange': ('#FF9800', 'Action Orange', 'Calls to action, active transfers, highlights'),
    'sky-blue': ('#42A5F5', 'Sky Blue', 'Brand blue on dark surfaces, progress and information'),
    'deep-grey': ('#121212', 'Deep Grey', 'Dark surfaces'),
    'light-grey-blue': ('#E0F2F1', 'Light Grey-Blue', 'Light surfaces and text on dark'),
    'ink': ('#0A1F44', 'Ink', 'Wordmark and headings on light surfaces'),
}
SCALES = {
    'blue': ['#E8F0FB', '#C5D7F2', '#8FB2E6', '#5288D6', '#2563C2', '#0D47A1', '#0B3C88', '#08306D', '#062452', '#041838'],
    'orange': ['#FFF5E6', '#FFE3BA', '#FFCC80', '#FFB74D', '#FFA726', '#FF9800', '#F08A00', '#D17600', '#A65E00', '#7A4500'],
    'sky': ['#EAF4FE', '#C9E3FC', '#A3CFFA', '#78B9F8', '#5DAEF6', '#42A5F5', '#2E8FE0', '#2277C4', '#1A5FA0', '#12477A'],
    'grey': ['#F4F8F8', '#E0F2F1', '#C9D6D8', '#A3B1B5', '#7A888D', '#566368', '#3A4448', '#262D31', '#1A1F22', '#121212'],
}
STEPS = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900]
STATUS = {'success': '#2E7D32', 'warning': '#FF9800', 'danger': '#D32F2F', 'info': '#42A5F5'}

BLUE, ORANGE, SKY, DEEP, MIST, INK = (PALETTE[k][0] for k in PALETTE)
FROST = '#E3EDF7'

# Mark: a return arrow whose lower bar flows, as an S, into the top bar and bowl of a P, while a stem drops from
# the curve, turns at the bottom and climbs back as an upload arrow. Traced on a 100 grid from the reference.

STROKE = 7
ARROW = (
    '<path d="M32.5 18H21.25a9.5 9.5 0 0 0 0 19H32" stroke="currentColor" stroke-width="{w}" fill="none"/>'
    '<path d="M31.5 9.5 47 18.25 31.5 27z" fill="currentColor"/>'
)
RIBBON = (
    '<path d="M57.5 37.5h19.25a9.75 9.75 0 0 0 0-19.5H60C50 18 45 37 31 37M43 32c-2.5 3-4 6-4 12v29.75a9.75 9.75 0 0 0 19.5 0V59"'
    ' stroke="currentColor" stroke-width="{w}" fill="none"/>'
    '<path d="M49.5 59.5 58.5 45l9 14.5z" fill="currentColor"/>'
)


def mark(arrow: str, body: str, box: tuple[float, float, float] = (0, 0, 1)) -> str:
    """The mark as an SVG group, centered on the 64 grid; the arrow ends where the ribbon takes over."""
    x, y, s = box
    return (
        f'<g transform="translate({x} {y}) scale({s}) translate(32 32) scale(.68) translate(-49.1 -48.25)">'
        f'<g class="tf-arrow" color="{arrow}">{ARROW.format(w=STROKE)}</g>'
        f'<g class="tf-ribbon" color="{body}">{RIBBON.format(w=STROKE)}</g></g>'
    )


# Wordmark


class Type:
    def __init__(self, path: Path):
        self.font = TTFont(path)
        self.glyphs = self.font.getGlyphSet()
        self.upem = self.font['head'].unitsPerEm
        blob = hb.Blob.from_file_path(str(path))
        self.hb = hb.Font(hb.Face(blob))

    def run(self, text: str, size: float, x: float, y: float, tracking: float = 0) -> tuple[str, float]:
        """Outlines `text` at baseline (x, y); returns the path data and the advance."""
        buf = hb.Buffer()
        buf.add_str(text)
        buf.guess_segment_properties()
        hb.shape(self.hb, buf, {'kern': True, 'liga': True})
        scale = size / self.upem
        order = self.font.getGlyphOrder()
        pen_x = 0.0
        d = []
        for info, pos in zip(buf.glyph_infos, buf.glyph_positions):
            pen = SVGPathPen(self.glyphs, ntos=lambda n: f'{n:.2f}'.rstrip('0').rstrip('.'))
            tx = x + (pen_x + pos.x_offset) * scale
            self.glyphs[order[info.codepoint]].draw(TransformPen(pen, (scale, 0, 0, -scale, tx, y - pos.y_offset * scale)))
            d.append(pen.getCommands())
            pen_x += pos.x_advance + tracking * self.upem
        return ' '.join(p for p in d if p), (pen_x - tracking * self.upem) * scale

    def cap(self, size: float) -> float:
        return self.font['OS/2'].sCapHeight * size / self.upem


def wordmark(t: Type, size: float, x: float, y: float, head: str, tail: str) -> tuple[str, float]:
    """"Transfer" + "ly", with the tail in the brand color; returns the SVG and the width."""
    tracking = -0.02
    d1, w1 = t.run('Transfer', size, x, y, tracking)
    d2, w2 = t.run('ly', size, x + w1 + tracking * size, y, tracking)
    return f'<path fill="{head}" d="{d1}"/><path fill="{tail}" d="{d2}"/>', w1 + tracking * size + w2


def svg(w: float, h: float, body: str, title: str) -> str:
    return f'<svg xmlns="http://www.w3.org/2000/svg" width="{w:g}" height="{h:g}" viewBox="0 0 {w:g} {h:g}" role="img" aria-label="{title}"><title>{title}</title>{body}</svg>\n'


def logos(bold: Type, regular: Type, out: Path) -> None:
    out.mkdir(parents=True, exist_ok=True)
    size = 50
    cap = bold.cap(size)
    pad = 8

    def horizontal(arrow, body, head, tail, tagline=None):
        mark_h = 64
        base = pad + (mark_h + cap) / 2 + 2
        text_x = pad + 64 + 14
        words, w = wordmark(bold, size, text_x, base, head, tail)
        width = text_x + w + pad
        height = mark_h + 2 * pad
        extra = ''
        if tagline:
            tag, tw = regular.run('Seamless data flow between all your connections.', 15.5, text_x + 1, base + 26)
            extra = f'<path fill="{tagline}" d="{tag}"/>'
            width = max(width, text_x + tw + pad)
            height += 18
        return width, height, mark(arrow, body, (pad, pad, 1)) + words + extra

    variants = {
        'logo-horizontal.svg': (BLUE, ORANGE, INK, BLUE),
        'logo-horizontal-on-dark.svg': (SKY, ORANGE, MIST, SKY),
        'logo-mono.svg': ('currentColor', 'currentColor', 'currentColor', 'currentColor'),
    }
    for name, (a, b, h, t) in variants.items():
        w, hgt, body = horizontal(a, b, h, t)
        (out / name).write_text(svg(w, hgt, body, 'Transferly'))

    w, hgt, body = horizontal(BLUE, ORANGE, INK, BLUE, tagline='#3A4448')
    (out / 'logo-tagline.svg').write_text(svg(w, hgt, body, 'Transferly — Seamless data flow between all your connections.'))

    # Vertical: mark above the wordmark.
    def vertical(arrow, body, head, tail):
        mark_s = 1.5
        _, w = wordmark(bold, size, 0, 0, head, tail)
        width = w + 2 * pad
        mx = (width - 64 * mark_s) / 2
        base = pad + 64 * mark_s + 10 + cap
        words, _ = wordmark(bold, size, pad, base, head, tail)
        return width, base + pad + 4, mark(arrow, body, (mx, pad, mark_s)) + words

    for name, (a, b, h, t) in {'logo-vertical.svg': (BLUE, ORANGE, INK, BLUE), 'logo-vertical-on-dark.svg': (SKY, ORANGE, MIST, SKY)}.items():
        w, hgt, body = vertical(a, b, h, t)
        (out / name).write_text(svg(w, hgt, body, 'Transferly'))

    # The mark alone, 64 grid.
    for name, (a, b) in {'mark.svg': (BLUE, ORANGE), 'mark-on-dark.svg': (SKY, ORANGE), 'mark-mono.svg': ('currentColor', 'currentColor')}.items():
        (out / name).write_text(svg(64, 64, mark(a, b), 'Transferly'))


def app_icons(out: Path) -> None:
    out.mkdir(parents=True, exist_ok=True)
    inset = (32 - 32 * 0.72, 32 - 32 * 0.72, 0.72)
    grad = f'<defs><linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#1565C0"/><stop offset="1" stop-color="{BLUE}"/></linearGradient></defs>'
    (out / 'app-icon.svg').write_text(svg(64, 64, f'{grad}<rect width="64" height="64" rx="14" fill="url(#bg)"/>' + mark(FROST, ORANGE, inset), 'Transferly'))
    (out / 'app-icon-dark.svg').write_text(svg(64, 64, f'<circle cx="32" cy="32" r="32" fill="#1A1F22"/><circle cx="32" cy="32" r="31.25" fill="none" stroke="#262D31" stroke-width="1.5"/>' + mark(SKY, ORANGE, (32 - 32 * 0.74, 32 - 32 * 0.74, 0.74)), 'Transferly'))
    (out / 'app-icon-maskable.svg').write_text(svg(64, 64, f'{grad}<rect width="64" height="64" fill="url(#bg)"/>' + mark(FROST, ORANGE, (32 - 32 * 0.62, 32 - 32 * 0.62, 0.62)), 'Transferly'))
    # Favicon: transparent, the mark adapts to the browser theme.
    favicon = (
        '<style>@media (prefers-color-scheme: dark){.tf-arrow{color:' + SKY + '}}</style>'
        + mark(BLUE, ORANGE)
    )
    (out / 'favicon.svg').write_text(svg(64, 64, favicon, 'Transferly'))


# Icons: 24 grid, 1.75 strokes in currentColor, one accent per icon in --tf-accent (Action Orange by default).

A = 'var(--tf-accent, #FF9800)'
ICONS = {
    'transfer': f'<path d="M4 8h14M14 4l4 4-4 4"/><path d="M20 16H6M10 12l-4 4 4 4" style="stroke:{A}"/>',
    'upload': f'<path d="M4 15v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3"/><path d="M12 15V4M7.5 8.5 12 4l4.5 4.5" style="stroke:{A}"/>',
    'download': f'<path d="M4 15v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3"/><path d="M12 4v11M7.5 10.5 12 15l4.5-4.5" style="stroke:{A}"/>',
    'sftp': f'<rect x="3" y="3" width="15" height="7" rx="2"/><path d="M3 15.5v-1A2.5 2.5 0 0 1 5.5 12H11"/><path d="M6.5 6.5h.01M6.5 15.5h.01" stroke-width="2.25"/><rect x="13" y="15" width="8" height="6" rx="1.5" style="stroke:{A}"/><path d="M15 15v-1.5a2 2 0 0 1 4 0V15" style="stroke:{A}"/>',
    'ftp': f'<rect x="3" y="3" width="15" height="7" rx="2"/><rect x="3" y="12" width="9" height="7" rx="2"/><path d="M6.5 6.5h.01M6.5 15.5h.01" stroke-width="2.25"/><circle cx="17.5" cy="17" r="3.5" style="stroke:{A}"/><path d="M15 19.5l5-5" style="stroke:{A}"/>',
    'ftps': f'<rect x="3" y="3" width="15" height="7" rx="2"/><path d="M3 15.5v-1A2.5 2.5 0 0 1 5.5 12H11"/><path d="M6.5 6.5h.01M6.5 15.5h.01" stroke-width="2.25"/><path d="M17 13l4 1.5V17c0 2.2-1.7 3.6-4 4.3-2.3-.7-4-2.1-4-4.3v-2.5z" style="stroke:{A}"/>',
    'cloud': f'<path d="M7 18a4 4 0 0 1-.6-7.95A6 6 0 0 1 18 9a4.5 4.5 0 0 1-.5 9H14"/><path d="M10 21v-7M7.5 16.5 10 14l2.5 2.5" style="stroke:{A}"/>',
    'sites': f'<rect x="9" y="3" width="6" height="5" rx="1"/><rect x="3" y="16" width="6" height="5" rx="1"/><rect x="15" y="16" width="6" height="5" rx="1"/><path d="M12 8v4M6 16v-2a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v2" style="stroke:{A}"/>',
    'folder': f'<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><path d="M8 13h8" style="stroke:{A}"/>',
    'security': f'<path d="M12 3 4.5 6v5.5c0 4.5 3.2 8 7.5 9.5 4.3-1.5 7.5-5 7.5-9.5V6z"/><rect x="9" y="11" width="6" height="5" rx="1" style="stroke:{A}"/><path d="M10.25 11V9.75a1.75 1.75 0 0 1 3.5 0V11" style="stroke:{A}"/>',
    'vault': f'<rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="M6 20v1.5M18 20v1.5"/><circle cx="12" cy="12" r="3.5" style="stroke:{A}"/><path d="M12 8.5V7M12 17v-1.5M15.5 12H17M7 12h1.5" style="stroke:{A}"/>',
    'logs': f'<rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="M3 8h18"/><path d="M7 12.5l2.5 2-2.5 2M12 16.5h5" style="stroke:{A}"/>',
    'settings': f'<path d="M10.3 3h3.4l.5 2.4a7 7 0 0 1 1.7 1l2.3-.8 1.7 3-1.8 1.6a7 7 0 0 1 0 1.9l1.8 1.6-1.7 3-2.3-.8a7 7 0 0 1-1.7 1l-.5 2.4h-3.4l-.5-2.4a7 7 0 0 1-1.7-1l-2.3.8-1.7-3 1.8-1.6a7 7 0 0 1 0-1.9L4.1 8.6l1.7-3 2.3.8a7 7 0 0 1 1.7-1z"/><circle cx="12" cy="12" r="2.75" style="stroke:{A}"/>',
    'user': f'<circle cx="12" cy="8" r="4"/><path d="M4.5 20a7.5 7.5 0 0 1 15 0" style="stroke:{A}"/>',
    'team': f'<circle cx="9" cy="8" r="3.5"/><path d="M2.5 19.5a6.5 6.5 0 0 1 13 0"/><path d="M15.5 4.8a3.5 3.5 0 0 1 0 6.4M18 14a6.5 6.5 0 0 1 3.5 5.5" style="stroke:{A}"/>',
    'queue': f'<path d="M4 6h10M4 12h10M4 18h6"/><path d="M18 10v10M15.5 17.5 18 20l2.5-2.5" style="stroke:{A}"/>',
    'resume': f'<path d="M20 12a8 8 0 1 1-2.35-5.65M20 4v4h-4"/><path d="M10 9v6l5-3z" style="stroke:{A};fill:{A}"/>',
    'connection-status': f'<circle cx="5" cy="12" r="2.5" style="stroke:{A};fill:{A}"/><circle cx="12" cy="12" r="2.5" style="stroke:{A};fill:{A}"/><circle cx="19" cy="12" r="2.5"/>',
}


def icons(out: Path) -> None:
    out.mkdir(parents=True, exist_ok=True)
    for name, body in ICONS.items():
        title = name.replace('-', ' ').capitalize()
        (out / f'{name}.svg').write_text(
            f'<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" role="img" aria-label="{title}"><title>{title}</title>{body}</svg>\n'
        )


def tokens(out: Path) -> None:
    out.mkdir(parents=True, exist_ok=True)
    data = {
        'brand': {k: {'value': v[0], 'name': v[1], 'use': v[2]} for k, v in PALETTE.items()},
        'scale': {name: dict(zip(map(str, STEPS), values)) for name, values in SCALES.items()},
        'status': STATUS,
        'font': {'sans': 'Geist', 'mono': 'Geist Mono'},
    }
    (out / 'tokens.json').write_text(json.dumps(data, indent=2) + '\n')
    lines = ['/* Transferly brand tokens. Generated by branding/scripts/build.py. */', ':root {']
    lines += [f'    --tf-{k}: {v[0]};' for k, v in PALETTE.items()]
    for name, values in SCALES.items():
        lines += [f'    --tf-{name}-{step}: {value};' for step, value in zip(STEPS, values)]
    lines += [f'    --tf-{k}: {v};' for k, v in STATUS.items()]
    lines += ['    --tf-accent: var(--tf-action-orange);', '}', '']
    (out / 'colors.css').write_text('\n'.join(lines))


def preview(out: Path) -> None:
    """A brand board that inlines every asset, so it opens straight from disk."""
    def inline(path: Path) -> str:
        return path.read_text().replace('<svg ', '<svg class="asset" ', 1)

    def tile(path: Path, label: str, dark: bool = False) -> str:
        cls = 'dark' if dark else ''
        return f'<figure class="{cls}">{inline(path)}<figcaption>{label}</figcaption></figure>'

    logo, app, icon = ROOT / 'logo', ROOT / 'app-icon', ROOT / 'icons'
    swatches = ''.join(
        f'<div class="swatch"><span style="background:{v[0]}"></span><b>{v[1]}</b><code>{v[0]}</code><small>{v[2]}</small></div>' for v in PALETTE.values()
    )
    scales = ''.join(
        f'<div class="scale"><b>{name}</b>' + ''.join(f'<span title="{name}-{step} {value}" style="background:{value}"></span>' for step, value in zip(STEPS, values)) + '</div>'
        for name, values in SCALES.items()
    )
    glyphs = ''.join(f'<figure class="icon">{inline(icon / (name + ".svg"))}<figcaption>{name}</figcaption></figure>' for name in ICONS)
    html = f'''<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Transferly brand</title><link rel="icon" href="app-icon/favicon.svg">
<style>
:root {{ --bg:#F4F8F8; --card:#FFFFFF; --text:#0A1F44; --muted:#566368; --line:#C9D6D8; }}
* {{ box-sizing:border-box }}
body {{ margin:0; background:var(--bg); color:var(--text); font:15px/1.5 Geist, ui-sans-serif, system-ui, sans-serif; }}
main {{ max-width:1180px; margin:0 auto; padding:48px 24px 80px; }}
h2 {{ font-size:13px; letter-spacing:.08em; text-transform:uppercase; color:var(--muted); margin:48px 0 16px; }}
.grid {{ display:grid; gap:16px; grid-template-columns:repeat(auto-fill, minmax(240px, 1fr)); }}
figure {{ margin:0; background:var(--card); border:1px solid var(--line); border-radius:14px; padding:28px; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:14px; min-height:170px; }}
figure.dark {{ background:#121212; border-color:#262D31; color:#E0F2F1; }}
figcaption {{ font-size:13px; color:var(--muted); }}
figure.dark figcaption {{ color:#A3B1B5; }}
.asset {{ max-width:100%; height:auto; }}
.hero {{ display:flex; flex-direction:column; gap:20px; }}
.hero .asset {{ width:min(520px, 100%); }}
.wide {{ grid-column:span 2; }}
.swatch {{ background:var(--card); border:1px solid var(--line); border-radius:14px; overflow:hidden; display:grid; padding-bottom:14px; }}
.swatch span {{ height:84px; margin-bottom:12px; }}
.swatch b, .swatch code, .swatch small {{ padding:0 14px; }}
.swatch small {{ color:var(--muted); }}
.scale {{ display:grid; grid-template-columns:90px repeat(10, 1fr); gap:4px; align-items:center; margin-bottom:6px; }}
.scale span {{ height:36px; border-radius:6px; }}
.icons {{ display:grid; gap:12px; grid-template-columns:repeat(auto-fill, minmax(110px, 1fr)); }}
figure.icon {{ min-height:110px; padding:18px; }}
figure.icon .asset {{ width:32px; height:32px; }}
.icons.dark figure {{ background:#121212; border-color:#262D31; color:#E0F2F1; }}
.type {{ display:grid; gap:16px; grid-template-columns:1fr 1fr; }}
.type div {{ background:var(--card); border:1px solid var(--line); border-radius:14px; padding:24px; }}
.mono {{ font-family:"Geist Mono", ui-monospace, monospace; }}
@media (max-width:640px) {{ .wide {{ grid-column:auto }} .type {{ grid-template-columns:1fr }} .scale {{ grid-template-columns:60px repeat(10, 1fr) }} }}
</style></head><body><main>
<section class="hero">{inline(logo / "logo-tagline.svg")}</section>

<h2>Logo</h2>
<div class="grid">
{tile(logo / "logo-horizontal.svg", "Horizontal")}
{tile(logo / "logo-horizontal-on-dark.svg", "Horizontal, on dark", True)}
{tile(logo / "logo-vertical.svg", "Vertical")}
{tile(logo / "logo-vertical-on-dark.svg", "Vertical, on dark", True)}
{tile(logo / "logo-mono.svg", "Monochrome (currentColor)")}
<figure class="dark" style="color:#FF9800">{inline(logo / "logo-mono.svg")}<figcaption>Monochrome, tinted</figcaption></figure>
</div>

<h2>Mark and application icon</h2>
<div class="grid">
{tile(app / "app-icon.svg", "App icon")}
{tile(app / "app-icon-dark.svg", "App icon, dark")}
{tile(app / "app-icon-maskable.svg", "Maskable (PWA)")}
{tile(app / "favicon.svg", "Favicon (follows the browser theme)")}
{tile(logo / "mark.svg", "Mark")}
{tile(logo / "mark-on-dark.svg", "Mark, on dark", True)}
</div>

<h2>Palette</h2>
<div class="grid">{swatches}</div>
<h2>Scales</h2>
{scales}

<h2>Typography</h2>
<div class="type">
<div><b>Geist</b> · interface<p style="font-size:28px;font-weight:700;margin:8px 0">Seamless data flow</p><p>ABCDEFGHIJKLMNOPQRSTUVWXYZ<br>abcdefghijklmnopqrstuvwxyz<br>0123456789</p></div>
<div class="mono"><b>Geist Mono</b> · logs, paths, sizes<p style="font-size:13px;margin:8px 0;color:var(--muted)">15:16:44 Connected to 13.38.112.129:22<br>15:16:45 Upload /var/www/index.html 100%</p><p>ABCDEFGHIJKLMNOPQRSTUVWXYZ<br>abcdefghijklmnopqrstuvwxyz<br>0123456789</p></div>
</div>

<h2>Icons</h2>
<div class="icons">{glyphs}</div>
<div class="icons dark" style="margin-top:12px">{glyphs}</div>
</main></body></html>
'''
    (out / 'preview.html').write_text(html)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument('--fonts', type=Path, required=True)
    args = parser.parse_args()
    bold = Type(args.fonts / 'Geist-Bold.ttf')
    regular = Type(args.fonts / 'Geist-Regular.ttf')
    logos(bold, regular, ROOT / 'logo')
    app_icons(ROOT / 'app-icon')
    icons(ROOT / 'icons')
    tokens(ROOT / 'tokens')
    preview(ROOT)


if __name__ == '__main__':
    main()
