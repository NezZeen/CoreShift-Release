"""Draws the tray icons: the app icon with the connection state on it.

The app icon is windows/runner/resources/app_icon.ico; the tray icons take
its 32 px frame and add a status dot. Run from app/: python tool/make_icons.py.
Needs only the standard library.
"""
import os
import struct
import zlib

APP_ICON = "windows/runner/resources/app_icon.ico"
SIZE = 32
DOT_OK = (0x34, 0xD3, 0x99)
DOT_BUSY = (0xFB, 0xBF, 0x24)


def frame(path, size):
    """RGBA pixels, top row first, of the 32-bit BMP frame of that size."""
    d = open(path, "rb").read()
    _, _, count = struct.unpack("<HHH", d[:6])
    for i in range(count):
        w, h, _, _, _, bpp, length, off = struct.unpack("<BBBBHHII", d[6 + 16 * i:22 + 16 * i])
        if w == size and h == size and bpp == 32 and d[off:off + 4] != b"\x89PNG":
            header = struct.unpack("<I", d[off:off + 4])[0]
            data = d[off + header:off + header + size * size * 4]
            rows = [data[y * size * 4:(y + 1) * size * 4] for y in range(size)][::-1]
            return [(r[x + 2], r[x + 1], r[x], r[x + 3]) for r in rows for x in range(0, size * 4, 4)]
    raise SystemExit(f"{path} has no 32-bit {size} px frame")


def grey(px):
    """Dimmed and without colour: not connected."""
    out = []
    for r, g, b, a in px:
        v = round(0.3 * r + 0.59 * g + 0.11 * b)
        v = round(v * .75 + 0x8B * .25)
        out.append((v, v, v, round(a * .85)))
    return out


def with_dot(px, size, colour, ss=4):
    """Puts a status dot in the bottom right corner, with a dark ring that
    keeps it apart from the logo."""
    cx = cy = size * .8
    r_dot, r_gap = size * .16, size * .22
    out = []
    for y in range(size):
        for x in range(size):
            dot = gap = 0
            for sy in range(ss):
                for sx in range(ss):
                    d2 = (x + (sx + .5) / ss - cx) ** 2 + (y + (sy + .5) / ss - cy) ** 2
                    if d2 <= r_dot ** 2:
                        dot += 1
                    elif d2 <= r_gap ** 2:
                        gap += 1
            dot, gap = dot / ss ** 2, gap / ss ** 2
            r, g, b, a = px[y * size + x]
            # The gap cuts the logo out; the dot is painted over what is left.
            a = round(a * (1 - gap - dot))
            base = (r, g, b, a)
            if dot:
                ta = dot * 255 + a * (1 - dot)
                mix = [(c * dot * 255 + bc * a * (1 - dot)) / ta for c, bc in zip(colour, base[:3])]
                out.append((round(mix[0]), round(mix[1]), round(mix[2]), round(ta)))
            else:
                out.append(base)
    return out


def png(size, px):
    raw = b"".join(b"\0" + b"".join(bytes(p) for p in px[y * size:(y + 1) * size]) for y in range(size))

    def chunk(tag, data):
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data))

    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def main():
    logo = frame(APP_ICON, SIZE)
    os.makedirs("assets/tray", exist_ok=True)
    variants = {
        "connected": with_dot(logo, SIZE, DOT_OK),
        "busy": with_dot(logo, SIZE, DOT_BUSY),
        "idle": grey(logo),
    }
    for name, px in variants.items():
        with open(f"assets/tray/{name}.png", "wb") as f:
            f.write(png(SIZE, px))


if __name__ == "__main__":
    main()
