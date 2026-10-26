#!/usr/bin/env python3
"""Значки JetBox для настольного приложения из канона JB (VPN_Box/marketing/LOGO.md).

Запуск: python3 app/tools/icons.py  (из корня репо; нужен Pillow, на macOS — iconutil).
"""
import os
import shutil
import subprocess
import sys
import tempfile

from PIL import Image, ImageChops, ImageDraw, ImageFilter

# Каталог Assets.xcassets приложения JetBox для iOS (источник знака и значка).
IOS = os.environ.get("JETBOX_IOS_ASSETS", "")
APP_ICON = f"{IOS}/AppIcon.appiconset/jetbox-icon-light.png"
MARK_LIGHT = f"{IOS}/logo-onboarding.imageset/logo-onboarding-light@3x.png"
MARK_DARK = f"{IOS}/logo-onboarding.imageset/logo-onboarding-dark@3x.png"

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
APP = os.path.join(ROOT, "app")


def rounded(im, radius_ratio=0.2237):
    """Скругление плашки (как у значков macOS/iOS)."""
    size = im.size[0]
    mask = Image.new("L", (size, size), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, size - 1, size - 1], radius=int(size * radius_ratio), fill=255)
    out = im.convert("RGBA")
    out.putalpha(mask)
    return out


def main():
    if not IOS or not os.path.isdir(IOS):
        sys.exit("задайте JETBOX_IOS_ASSETS — путь к Assets.xcassets приложения JetBox для iOS")
    src = rounded(Image.open(APP_ICON).convert("RGBA"))

    # Windows: многоразмерный .ico
    ico_dir = os.path.join(APP, "build", "windows")
    os.makedirs(ico_dir, exist_ok=True)
    src.save(os.path.join(ico_dir, "icon.ico"), sizes=[(s, s) for s in (16, 20, 24, 32, 40, 48, 64, 128, 256)])

    # macOS: .icns через iconutil
    mac_dir = os.path.join(APP, "build", "darwin")
    os.makedirs(mac_dir, exist_ok=True)
    if shutil.which("iconutil"):
        with tempfile.TemporaryDirectory() as tmp:
            iconset = os.path.join(tmp, "JetBox.iconset")
            os.makedirs(iconset)
            for pt in (16, 32, 128, 256, 512):
                for scale in (1, 2):
                    px = pt * scale
                    name = f"icon_{pt}x{pt}{'@2x' if scale == 2 else ''}.png"
                    src.resize((px, px), Image.LANCZOS).save(os.path.join(iconset, name))
            subprocess.run(["iconutil", "-c", "icns", iconset, "-o", os.path.join(mac_dir, "JetBox.icns")], check=True)
    else:
        print("iconutil нет — JetBox.icns не обновлён", file=sys.stderr)

    # Значок трея Windows и строки меню macOS — по состоянию, как у официального клиента:
    #   on    — буквы JB монохромом, инверсионный след фирменным синим (#1288e1);
    #   off   — всё монохромом;
    #   connecting — монохром, след приглушён;
    #   alert — монохром + точка цвета предупреждения («выход не отвечает», нет службы).
    # Каждое состояние в двух вариантах: для светлой панели (тёмный монохром) и тёмной (белый).
    # Квадрат 36x36 (18 pt @2x на macOS; Windows масштабирует под DPI): форк Wails задаёт
    # NSImage размер thickness x thickness, неквадратный значок сплющился бы.
    icons = os.path.join(APP, "icons")
    os.makedirs(icons, exist_ok=True)
    mark = Image.open(MARK_LIGHT).convert("RGBA")
    side, margin = 36, 1
    box = side - 2 * margin
    scale = min(box / mark.size[0], box / mark.size[1])
    w, h = max(1, round(mark.size[0] * scale)), max(1, round(mark.size[1] * scale))
    mark = mark.resize((w, h), Image.LANCZOS)
    letters = Image.new("L", mark.size, 0)
    trail = Image.new("L", mark.size, 0)
    lp, tp = letters.load(), trail.load()
    for y in range(h):
        for x in range(w):
            r, g, b, a = mark.getpixel((x, y))
            # След — синий (b заметно больше r), буквы — тёмно-синие.
            if b - r > 80:
                tp[x, y] = a
            else:
                lp[x, y] = a
    # В 16–18 px исходные полоски следа тоньше пикселя — утолщаем на пиксель и вырезаем
    # утолщение из букв, чтобы след не сливался с J.
    trail = trail.filter(ImageFilter.MaxFilter(3))
    letters = ImageChops.subtract(letters, trail)
    warn = (0xE8, 0xA3, 0x17)
    # Синий следа — фирменный акцент: на тёмной панели светлее (#1a9bf5), как в тёмной теме приложения.
    for variant, mono, blue in (("light", (0, 0, 0, 217), (0x12, 0x88, 0xE1)),
                                ("dark", (255, 255, 255, 255), (0x1A, 0x9B, 0xF5))):
        def layer(mask, rgb, opacity):
            img = Image.new("RGBA", (w, h), rgb[:3] + (0,))
            img.putalpha(mask.point(lambda a, o=opacity: a * o // 255))
            return img

        def canvas(*layers):
            full = Image.new("RGBA", (side, side), (0, 0, 0, 0))
            for l in layers:
                full.alpha_composite(l, ((side - w) // 2, (side - h) // 2))
            return full

        mono_letters = layer(letters, mono, mono[3])
        states = {
            "on": canvas(mono_letters, layer(trail, blue, 255)),
            "off": canvas(mono_letters, layer(trail, mono, mono[3])),
            "connecting": canvas(mono_letters, layer(trail, mono, mono[3] * 35 // 100)),
        }
        alert = states["off"].copy()
        d = ImageDraw.Draw(alert)
        r = 6
        cx, cy = side - r - 1, side - r - 1
        # Прозрачное кольцо вокруг точки — чтобы она читалась поверх буквы.
        clear = Image.new("L", (side, side), 255)
        ImageDraw.Draw(clear).ellipse([cx - r - 2, cy - r - 2, cx + r + 2, cy + r + 2], fill=0)
        alert.putalpha(Image.composite(alert.getchannel("A"), Image.new("L", (side, side), 0), clear))
        d = ImageDraw.Draw(alert)
        d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=warn + (255,))
        states["alert"] = alert
        for name, img in states.items():
            img.save(os.path.join(icons, f"tray-{name}-{variant}.png"))

    # Окно: значок приложения 256 px (Wails Options.Icon)
    src.resize((256, 256), Image.LANCZOS).save(os.path.join(icons, "app-256.png"))

    # Фронтенд: знак для светлой и тёмной темы
    assets = os.path.join(APP, "frontend", "src", "assets")
    os.makedirs(assets, exist_ok=True)
    shutil.copyfile(MARK_LIGHT, os.path.join(assets, "jb-light.png"))
    shutil.copyfile(MARK_DARK, os.path.join(assets, "jb-dark.png"))
    print("ok")


if __name__ == "__main__":
    main()
