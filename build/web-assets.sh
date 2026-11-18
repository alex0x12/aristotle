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
font() { curl -fsSL -o "$work/$1" "$2"; }
font Forum-Regular.ttf             "$gf/forum/Forum-Regular.ttf"
font EBGaramond.ttf                "$gf/ebgaramond/EBGaramond%5Bwght%5D.ttf"
font EBGaramond-Italic.ttf         "$gf/ebgaramond/EBGaramond-Italic%5Bwght%5D.ttf"
curl -fsSL -o "$out/fonts/OFL-Forum.txt"      "$gf/forum/OFL.txt"
curl -fsSL -o "$out/fonts/OFL-EBGaramond.txt" "$gf/ebgaramond/OFL.txt"

# Латиница, кириллица, общая пунктуация, №, ≈ (приблизительная сумма токенов);
# греческий и греческий расширенный (политоника: ἔθος) — только для
# EB Garamond: у Forum его нет.
base='U+0000-00FF,U+0100-017F,U+0300-036F,U+0400-04FF,U+2000-206F,U+2116,U+2248'
greek='U+0370-03FF,U+1F00-1FFF'
subset() {
	pyftsubset "$work/$1" --unicodes="$2" --layout-features='*' --output-file="$work/$3.ttf"
	woff2_compress "$work/$3.ttf" >/dev/null
	mv "$work/$3.woff2" "$out/fonts/$3.woff2"
}
subset Forum-Regular.ttf     "$base"          forum
subset EBGaramond.ttf        "$base,$greek"   eb-garamond
subset EBGaramond-Italic.ttf "$base,$greek"   eb-garamond-italic

ls -l "$out/img" "$out/fonts"
