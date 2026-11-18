#!/usr/bin/env bash
# Изображения и шрифты веб-интерфейса: скачать и подготовить к встраиванию.
# Источники и лицензии — SOURCES.md, раздел «Веб-интерфейс». Нужны curl,
# pyftsubset (fonttools) и woff2_compress; результат лежит в git, так что
# запускать нужно, только если меняется набор изображений или шрифтов.
set -euo pipefail
cd "$(dirname "$0")/.."

out=internal/web/static
work=build/.cache/web-assets
ua='aristotle-assets/1.0 (https://github.com/alex0x12/aristotle)'
mkdir -p "$out/img" "$out/fonts" "$work"

# Миниатюры Wikimedia отдаются только стандартной ширины: 1280 и 500 есть,
# 640 возвращает страницу ошибки.
img() { curl -fsSL -A "$ua" -o "$out/img/$1" "$2"; }
img school-of-athens.jpg  'https://thumb.wikimedia.org/wikipedia/commons/thumb/4/49/%22The_School_of_Athens%22_by_Raffaello_Sanzio_da_Urbino.jpg/1280px-%22The_School_of_Athens%22_by_Raffaello_Sanzio_da_Urbino.jpg'
img ergastines-frieze.jpg 'https://thumb.wikimedia.org/wikipedia/commons/thumb/d/de/Egastinai_frieze_Louvre_MR825.jpg/1280px-Egastinai_frieze_Louvre_MR825.jpg'
img aristotle-bust.png    'https://thumb.wikimedia.org/wikipedia/commons/thumb/2/24/Aristotle_Altemps_Inv8575_%28png%29.png/500px-Aristotle_Altemps_Inv8575_%28png%29.png'
img exekias-amphora.jpg   'https://thumb.wikimedia.org/wikipedia/commons/thumb/a/a8/Achilles_and_Ajax_playing_dice%2C_detail%2C_Attic_black-figured_amphora%2C_signed_by_Exekias_as_both_painter_and_potter%2C_540-530_BC%2C_inv._16757_-_Museo_Gregoriano_Etrusco_-_Vatican_Museums_-_DSC01051_%28cropped%29.jpg/1280px-thumbnail.jpg'
img academy-mosaic.jpg    'https://thumb.wikimedia.org/wikipedia/commons/thumb/8/84/MANNapoli_124545_plato%27s_academy_mosaic_%28cropped%29.jpg/1280px-MANNapoli_124545_plato%27s_academy_mosaic_%28cropped%29.jpg'

gf=https://raw.githubusercontent.com/google/fonts/main/ofl
font() { curl -fsSL -A "$ua" -o "$work/$1" "$2"; }
# Alegreya — единственный шрифт интерфейса: два переменных файла (прямой и
# курсив) с осью веса wght 400–900.
font Alegreya.ttf        "$gf/alegreya/Alegreya%5Bwght%5D.ttf"
font Alegreya-Italic.ttf "$gf/alegreya/Alegreya-Italic%5Bwght%5D.ttf"
curl -fsSL -A "$ua" -o "$out/fonts/OFL-Alegreya.txt" "$gf/alegreya/OFL.txt"

# Латиница, кириллица, общая пунктуация, №, ≈ (приблизительная сумма токенов),
# греческий и греческий расширенный (политоника: ἔθος; заглавные логотипа).
base='U+0000-00FF,U+0100-017F,U+0300-036F,U+0400-04FF,U+2000-206F,U+2116,U+2248'
greek='U+0370-03FF,U+1F00-1FFF'
# Ось веса должна пережить подрезку: без неё 500 и 600 нарисуются одинаково,
# и подписи потеряют вес, ради которого шрифт и менялся.
has_wght() {
	python3 -I -c 'import sys; from fontTools.ttLib import TTFont; f = TTFont(sys.argv[1]); sys.exit(0 if "fvar" in f and any(a.axisTag == "wght" for a in f["fvar"].axes) else 1)' "$1"
}
subset() {
	pyftsubset "$work/$1" --unicodes="$2" --layout-features='*' --output-file="$work/$3.ttf"
	has_wght "$work/$3.ttf" || { echo "подрезка потеряла ось wght: $3" >&2; exit 1; }
	woff2_compress "$work/$3.ttf" >/dev/null
	mv "$work/$3.woff2" "$out/fonts/$3.woff2"
}
subset Alegreya.ttf        "$base,$greek" alegreya
subset Alegreya-Italic.ttf "$base,$greek" alegreya-italic

ls -l "$out/img" "$out/fonts"
