"""Draws every CoreShift icon from its mark.

The mark is the one the app paints (CoreShiftMark in lib/ui/widgets.dart): a
railway switch, the straight track and the one it shifts to, dark on an amber
rounded square, on a 24-unit grid. Its geometry lives below and is also
written out as assets/icon/coreshift.svg, the master for other uses.

Writes:
  assets/icon/coreshift.svg, coreshift-512.png    the master and a preview
  windows/runner/resources/app_icon.ico            16-256 px, also the installer's
  assets/tray/{idle,busy,connected}.png            tray icons: the state on the mark
  android/app/src/main/res/mipmap-*/               legacy, round and adaptive layers
  web/favicon.png, web/icons/*                     the web demo
  linux/icons/hicolor/*/apps/coreshift.*           Linux: the menu entry and the window

The Android notification icon (res/drawable/ic_stat_vpn.xml) is a vector of
the same glyph, kept by hand.

Run from app/: python tool/make_icons.py. Needs only the standard library.
"""
import math
import os
import struct
import zlib

ACCENT = (0xF0, 0xA2, 0x3B)
ON_ACCENT = (0x24, 0x16, 0x04)
DOT_OK = (0x3C, 0xCB, 0x98)
DOT_BUSY = (0xF2, 0xCB, 0x4C)

# The mark on its 24-unit grid; the two tracks lie on Y_TOP and Y_BOTTOM.
GRID, RADIUS, WIDTH = 24, 6, 2.4
Y_TOP, Y_BOTTOM = 8.5, 15.5
STRAIGHT = [(5, Y_TOP), (19, Y_TOP)]
SWITCH_START, SWITCH_CURVE, SWITCH_END = (7.5, Y_TOP), ((11, Y_TOP), (11.5, Y_BOTTOM), (15, Y_BOTTOM)), (19, Y_BOTTOM)

SVG = f"""<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 {GRID} {GRID}">
  <!-- CoreShift: a railway switch on the amber of the lamps. Drawn by tool/make_icons.py. -->
  <rect width="{GRID}" height="{GRID}" rx="{RADIUS}" fill="#{"%02X%02X%02X" % ACCENT}"/>
  <g fill="none" stroke="#{"%02X%02X%02X" % ON_ACCENT}" stroke-width="{WIDTH}" stroke-linecap="round" stroke-linejoin="round">
    <path d="M{STRAIGHT[0][0]} {STRAIGHT[0][1]}H{STRAIGHT[1][0]}"/>
    <path d="M{SWITCH_START[0]} {SWITCH_START[1]}C{SWITCH_CURVE[0][0]} {SWITCH_CURVE[0][1]} {SWITCH_CURVE[1][0]} {SWITCH_CURVE[1][1]} \
{SWITCH_CURVE[2][0]} {SWITCH_CURVE[2][1]}H{SWITCH_END[0]}"/>
  </g>
</svg>
"""


def clamp(v):
    return 0.0 if v < 0 else 1.0 if v > 1 else v


class Canvas:
    """Straight-alpha RGBA in floats; shapes are painted by their coverage."""

    def __init__(self, size):
        self.size = size
        self.px = [[0.0, 0.0, 0.0, 0.0] for _ in range(size * size)]

    def paint(self, coverage, colour, box=None):
        """Paints colour where coverage(x, y) of the pixel centre is above 0."""
        x0, y0, x1, y1 = self._box(box)
        cr, cg, cb = (c / 255 for c in colour)
        for y in range(y0, y1):
            for x in range(x0, x1):
                c = coverage(x + .5, y + .5)
                if c <= 0:
                    continue
                p = self.px[y * self.size + x]
                a = c + p[3] * (1 - c)
                p[0] = (cr * c + p[0] * p[3] * (1 - c)) / a
                p[1] = (cg * c + p[1] * p[3] * (1 - c)) / a
                p[2] = (cb * c + p[2] * p[3] * (1 - c)) / a
                p[3] = a

    def cut(self, coverage, box=None):
        x0, y0, x1, y1 = self._box(box)
        for y in range(y0, y1):
            for x in range(x0, x1):
                c = coverage(x + .5, y + .5)
                if c > 0:
                    self.px[y * self.size + x][3] *= 1 - c

    def _box(self, box):
        if box is None:
            return 0, 0, self.size, self.size
        x0, y0, x1, y1 = box
        s = self.size
        return max(0, int(x0) - 1), max(0, int(y0) - 1), min(s, int(math.ceil(x1)) + 1), min(s, int(math.ceil(y1)) + 1)

    def rgba(self):
        return [tuple(round(clamp(v) * 255) for v in p) for p in self.px]


def rounded_rect(x0, y0, x1, y1, r):
    cx, cy, hx, hy = (x0 + x1) / 2, (y0 + y1) / 2, (x1 - x0) / 2, (y1 - y0) / 2

    def coverage(x, y):
        qx, qy = abs(x - cx) - hx + r, abs(y - cy) - hy + r
        d = math.hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - r
        return clamp(.5 - d)

    return coverage


def circle(cx, cy, r):
    return lambda x, y: clamp(.5 - (math.hypot(x - cx, y - cy) - r))


def glyph(k, ox, oy):
    """The two tracks with the mark's grid scaled by k px and moved to
    (ox, oy): their coverage and bounding box. Below 49 px a mark gets whole
    pixel strokes on pixel rows, so the small icons stay sharp."""
    width = WIDTH * k
    y_top, y_bottom = oy + Y_TOP * k, oy + Y_BOTTOM * k
    if GRID * k < 49:
        width = max(2, round(width))
        f = 0 if width % 2 == 0 else .5
        # Halves round towards the middle, so both tracks move alike.
        y_top, y_bottom = math.floor(y_top - f + .5) + f, math.ceil(y_bottom - f - .5) + f

    def at(p):
        u, v = p
        return ox + u * k, y_top + (v - Y_TOP) / (Y_BOTTOM - Y_TOP) * (y_bottom - y_top)

    p0, (p1, p2, p3) = at(SWITCH_START), map(at, SWITCH_CURVE)
    n = 48
    curve = []
    for i in range(n + 1):
        t = i / n
        a, b, c, d = (1 - t) ** 3, 3 * (1 - t) ** 2 * t, 3 * (1 - t) * t * t, t ** 3
        curve.append((a * p0[0] + b * p1[0] + c * p2[0] + d * p3[0], a * p0[1] + b * p1[1] + c * p2[1] + d * p3[1]))
    curve.append(at(SWITCH_END))
    lines = [[at(p) for p in STRAIGHT], curve]
    segments = [(a, b) for line in lines for a, b in zip(line, line[1:])]
    half = width / 2

    def coverage(x, y):
        best = 1e9
        for (ax, ay), (bx, by) in segments:
            dx, dy = bx - ax, by - ay
            ll = dx * dx + dy * dy
            t = 0 if ll == 0 else max(0, min(1, ((x - ax) * dx + (y - ay) * dy) / ll))
            ex, ey = x - ax - t * dx, y - ay - t * dy
            d = ex * ex + ey * ey
            if d < best:
                best = d
        return clamp(half - math.sqrt(best) + .5)

    xs = [p[0] for s in segments for p in s]
    ys = [p[1] for s in segments for p in s]
    return coverage, (min(xs) - half, min(ys) - half, max(xs) + half, max(ys) + half)


def mark(size, pad=0.0, shape="square"):
    """The whole mark: the glyph on amber, in a square of size px with pad
    px of transparency around it."""
    c = Canvas(size)
    side = size - 2 * pad
    if shape == "circle":
        c.paint(circle(size / 2, size / 2, side / 2), ACCENT)
    else:
        c.paint(rounded_rect(pad, pad, size - pad, size - pad, side * RADIUS / GRID), ACCENT)
    cov, box = glyph(side / GRID, pad, pad)
    c.paint(cov, ON_ACCENT, box)
    return c


def layer(size, background=None, grid=72 / 108):
    """The glyph alone, its mark's grid taking that share of the square. On
    Android's 108 dp layer the grid fills the 72 dp the launcher shows, which
    keeps the glyph inside the 66 dp safe circle; a web maskable icon keeps it
    inside the middle 80%."""
    c = Canvas(size)
    if background:
        c.paint(lambda x, y: 1.0, background)
    pad = size * (1 - grid) / 2
    cov, box = glyph(size * grid / GRID, pad, pad)
    c.paint(cov, ON_ACCENT, box)
    return c


def grey(px):
    """Dimmed and without colour: not connected."""
    out = []
    for r, g, b, a in px:
        v = round(0.3 * r + 0.59 * g + 0.11 * b)
        v = round(v * .75 + 0x8B * .25)
        out.append((v, v, v, round(a * .85)))
    return out


def with_dot(canvas, colour):
    """A status dot in the bottom right corner, with a ring of transparency
    that keeps it apart from the mark."""
    s = canvas.size
    c = s * .8
    canvas.cut(circle(c, c, s * .22))
    canvas.paint(circle(c, c, s * .16), colour)
    return canvas.rgba()


def png(size, px):
    raw = b"".join(b"\0" + b"".join(bytes(p) for p in px[y * size:(y + 1) * size]) for y in range(size))

    def chunk(tag, data):
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data))

    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def bmp_frame(size, px):
    """An icon frame as a 32-bit DIB with its AND mask, bottom row first."""
    header = struct.pack("<IiiHHIIiiII", 40, size, size * 2, 1, 32, 0, 0, 0, 0, 0, 0)
    rows = [px[y * size:(y + 1) * size] for y in range(size)][::-1]
    colour = b"".join(bytes((b, g, r, a)) for row in rows for r, g, b, a in row)
    stride = (size + 31) // 32 * 4
    mask = b""
    for row in rows:
        bits = bytearray(stride)
        for x, p in enumerate(row):
            if p[3] == 0:
                bits[x // 8] |= 0x80 >> (x % 8)
        mask += bytes(bits)
    return header + colour + mask


def ico(frames):
    """frames: (size, data); 256 px goes as PNG, the rest as DIBs."""
    out = struct.pack("<HHH", 0, 1, len(frames))
    offset = 6 + 16 * len(frames)
    body = b""
    for size, data in frames:
        out += struct.pack("<BBBBHHII", size % 256, size % 256, 0, 0, 1, 32, len(data), offset + len(body))
        body += data
    return out + body


def write(path, data):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as f:
        f.write(data)
    print(path)


def write_png(path, canvas):
    write(path, png(canvas.size, canvas.rgba()))


def main():
    write("assets/icon/coreshift.svg", SVG.encode())
    write_png("assets/icon/coreshift-512.png", mark(512))

    frames = []
    for s in (16, 20, 24, 32, 40, 48, 64, 256):
        px = mark(s).rgba()
        frames.append((s, png(s, px) if s == 256 else bmp_frame(s, px)))
    write("windows/runner/resources/app_icon.ico", ico(frames))

    write("assets/tray/idle.png", png(32, grey(mark(32).rgba())))
    write("assets/tray/busy.png", png(32, with_dot(mark(32), DOT_BUSY)))
    write("assets/tray/connected.png", png(32, with_dot(mark(32), DOT_OK)))

    res = "android/app/src/main/res"
    for name, scale in (("mdpi", 1), ("hdpi", 1.5), ("xhdpi", 2), ("xxhdpi", 3), ("xxxhdpi", 4)):
        s = round(48 * scale)
        write_png(f"{res}/mipmap-{name}/ic_launcher.png", mark(s, pad=2 * scale))
        write_png(f"{res}/mipmap-{name}/ic_launcher_round.png", mark(s, pad=2 * scale, shape="circle"))
        write_png(f"{res}/mipmap-{name}/ic_launcher_foreground.png", layer(round(108 * scale)))

    # Linux: the desktop entry's and the window's icon, in the sizes the
    # hicolor theme looks for (packaging/linux installs them).
    for s in (16, 24, 32, 48, 64, 128, 256, 512):
        write_png(f"linux/icons/hicolor/{s}x{s}/apps/coreshift.png", mark(s))
    write("linux/icons/hicolor/scalable/apps/coreshift.svg", SVG.encode())

    write_png("web/favicon.png", mark(32))
    for s in (192, 512):
        write_png(f"web/icons/Icon-{s}.png", mark(s))
        write_png(f"web/icons/Icon-maskable-{s}.png", layer(s, background=ACCENT, grid=.9))


if __name__ == "__main__":
    main()
