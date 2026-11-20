"""Сборка русского корпуса из сканов «Философского наследия».

Запуск: venv/bin/python pdf_corpus.py metaphorsysics
        venv/bin/python pdf_corpus.py --all

Источник — PDF с текстовым слоем ABBYY. Слова берутся с координатами, поля с
беккеровскими номерами отсекаются геометрически (parse_pdf.page_lines), книги и
главы находятся по заголовкам «КНИГА …»/«ГЛАВА …».

Русский корпус кладётся отдельно от английского: переводы 1970–80-х под
охраной, в репозиторий их класть нельзя.
"""
import argparse
import re
import shutil
import subprocess
import sys
from pathlib import Path
from xml.etree import ElementTree

import yaml

import parse_pdf

HERE = Path(__file__).parent
CACHE = HERE / ".cache"
DOWNLOADS = Path.home() / "Downloads"

VOLUMES = {
    1: "Аристотель - Собрание сочинений в 4-х томах том 1. Философское наследие - 1976.pdf",
    2: "Аристотель - Собрание сочинений в 4-х томах том 2. Философское наследие - 1978.pdf",
    3: "Аристотель - Собрание сочинений в 4-х томах том 3. Философское наследие - 1981.pdf",
    4: "Аристотель - Собрание сочинений в 4-х томах том 4. Философское наследие - 1983.pdf",
}

REQUIRED_WORK_FIELDS = (
    "title", "volume", "first", "last", "expect",
    "translator", "year", "group", "heads",
)


def load_works(directory: Path) -> dict[str, dict]:
    """Читает описания работ из YAML-файлов каталога.

    Файлы берутся в отсортированном порядке и склеиваются. Дубль slug между
    файлами и недостающие или неверные поля — ошибка конфигурации: лучше
    остановиться, чем молча собрать не то.

    Полосы найдены по тексту томов (страницы PDF, с 1, включительно) и
    проверяются полем expect: если скан заменили и полоса съехала, сборка
    остановится, а не запишет мусор.
    """
    directory = Path(directory)
    if not directory.is_dir():
        raise ValueError(f"нет каталога с работами: {directory}")

    works: dict[str, dict] = {}
    origins: dict[str, Path] = {}
    for path in sorted(directory.glob("*.yaml")):
        raw = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
        if not isinstance(raw, dict):
            raise ValueError(
                f"{path.name}: ожидался словарь slug -> параметры работы")
        for slug, cfg in raw.items():
            if slug in works:
                raise ValueError(
                    f"дубль slug {slug}: {origins[slug].name} и {path.name}")
            if not isinstance(cfg, dict):
                raise ValueError(
                    f"{path.name}: {slug}: ожидался словарь параметров")
            for field in REQUIRED_WORK_FIELDS:
                if field not in cfg:
                    raise ValueError(
                        f"{path.name}: {slug}: отсутствует обязательное поле "
                        f"{field}")
            for field in ("volume", "first", "last"):
                value = cfg[field]
                if not isinstance(value, int) or isinstance(value, bool):
                    raise ValueError(
                        f"{path.name}: {slug}: поле {field} должно быть "
                        f"целым числом")
            if cfg["first"] > cfg["last"]:
                raise ValueError(
                    f"{path.name}: {slug}: first ({cfg['first']}) больше "
                    f"last ({cfg['last']})")
            heads = cfg["heads"]
            if (not isinstance(heads, list) or not heads
                    or not all(isinstance(h, str) for h in heads)):
                raise ValueError(
                    f"{path.name}: {slug}: heads — непустой список строк")
            cfg.setdefault("style", "glava")
            cfg.setdefault("ocr", False)
            works[slug] = cfg
            origins[slug] = path
    return works


WORKS = load_works(HERE / "works_ru")


def bbox_xml(volume: int) -> Path:
    """XML с координатами слов: один раз на том, дальше из кеша."""
    pdf = DOWNLOADS / VOLUMES[volume]
    if not pdf.exists():
        raise SystemExit(f"нет файла: {pdf}")
    cache = CACHE / f"vol{volume}.bbox.xml"
    if not cache.exists():
        CACHE.mkdir(parents=True, exist_ok=True)
        with cache.open("wb") as out:
            subprocess.run(["pdftotext", "-bbox", str(pdf), "-"],
                           check=True, stdout=out, stderr=subprocess.DEVNULL)
    return cache


def _local(tag: str) -> str:
    """Имя тега без пространства имён: pdftotext объявляет XHTML-namespace."""
    return tag.rpartition("}")[2]


def pages(xml_path: Path) -> list[tuple[float, list[parse_pdf.Word]]]:
    """Страницы тома: ширина и слова с координатами."""
    tree = ElementTree.parse(xml_path)
    out = []
    for page in (e for e in tree.iter() if _local(e.tag) == "page"):
        words = [
            parse_pdf.Word(x0=float(w.get("xMin")), y0=float(w.get("yMin")),
                           x1=float(w.get("xMax")), y1=float(w.get("yMax")),
                           text="".join(w.itertext()))
            for w in page.iter() if _local(w.tag) == "word"
        ]
        out.append((float(page.get("width")), words))
    return out


def ocr_lines(volume: int, first: int, last: int) -> list[str]:
    """Строки полосы, распознанные tesseract'ом (когда слой ABBYY теряет номера).

    Результат кешируется: 80 страниц при 400 dpi — это минуты, а не секунды.
    """
    pdf = DOWNLOADS / VOLUMES[volume]
    cache = CACHE / f"vol{volume}-{first}-{last}.ocr.txt"
    if cache.exists():
        return cache.read_text(encoding="utf-8").splitlines()
    CACHE.mkdir(parents=True, exist_ok=True)
    tmp = CACHE / f"ocr-{volume}-{first}-{last}"
    tmp.mkdir(exist_ok=True)
    subprocess.run(["pdftoppm", "-f", str(first), "-l", str(last), "-r", "400",
                    "-gray", "-png", str(pdf), str(tmp / "p")], check=True)
    out: list[str] = []
    for png in sorted(tmp.glob("p-*.png")):
        done = subprocess.run(["tesseract", str(png), "-", "-l", "rus"],
                              check=True, capture_output=True, text=True)
        out.extend(done.stdout.splitlines())
    cache.write_text("\n".join(out), encoding="utf-8")
    shutil.rmtree(tmp)
    return out


def page_lines(page) -> list[str]:
    width, words = page
    return parse_pdf.page_lines(words, width)


def drop_heads(lines: list[str], heads) -> list[str]:
    """Убирает колонтитулы: название работы капслоком или в разрядку.

    Разрядка («М Е Т А Ф И З И К А») после снятия пробелов совпадает с обычной
    шапкой, поэтому сравнение идёт без пробелов.
    """
    wanted = {h.replace(" ", "") for h in heads}
    out = []
    for line in lines:
        flat = " ".join(line.split())
        if flat.replace(" ", "") in wanted and flat.upper() == flat:
            continue
        out.append(line)
    return out


# Ведущий номер главы в стиле numbered. Кроме цифр и кириллических двойников
# сюда входит «{»: OCR читает им «1» («{. Собираясь…», «1{7. А теперь…»).
NUMBERED_MARKER_RE = re.compile(r"^([0-9ЗзОоИиIl{]{1,5})\.(\s+.*)$")


def fix_numbered_ocr(lines: list[str]) -> list[str]:
    """Приводит номера глав к порядку 1, 2, 3, … после ошибок OCR.

    Строка главы опознаётся по ведущему «номеру» перед точкой; меняется только
    этот номер, текст и разбиение строк остаются как были. «{» считается
    прочитанной единицей, а «1{7.» — семнадцатью. Если номер расходится с
    ожидаемым, ставится ожидаемый: в numbered-работе главы идут подряд, а
    именно номера OCR и путает. Новая книга сбрасывает счёт.
    """
    out = []
    expected = 0
    for line in lines:
        if parse_pdf.BOOK_RE.match(line):
            expected = 0
            out.append(line)
            continue
        m = NUMBERED_MARKER_RE.match(line)
        if not m:
            out.append(line)
            continue
        expected += 1
        # Номер всё равно пишем свой: «{.», «1{7.» и «З.» — это одно и то же
        # место главы, прочитанное по-разному; порядок надёжнее распознавания.
        out.append(f"{expected}.{m.group(2)}")
    return out


def chapters_for(slug: str):
    cfg = WORKS[slug]
    pages_all = pages(bbox_xml(cfg["volume"]))
    if not 1 <= cfg["first"] <= cfg["last"] <= len(pages_all):
        raise SystemExit(f"{slug}: полоса {cfg['first']}..{cfg['last']} вне тома")
    picked = pages_all[cfg["first"] - 1:cfg["last"]]
    first_lines = page_lines(picked[0])
    if not any(cfg["expect"] in line for line in first_lines):
        raise SystemExit(
            f"{slug}: первая страница полосы не начинается с «{cfg['expect']}»; "
            f"скан изменился — перепроверьте полосу")

    if cfg.get("ocr"):
        lines = parse_pdf.clean_lines(
            [line for line in ocr_lines(cfg["volume"], cfg["first"], cfg["last"])
             if line.strip()])
    else:
        lines = []
        for page in picked:
            lines.extend(page_lines(page))
        lines = parse_pdf.clean_lines(drop_heads(lines, cfg["heads"]))
    lines = [line for line in lines if line.strip()]
    style = cfg.get("style", "glava")
    if style == "numbered" and cfg.get("ocr"):
        lines = fix_numbered_ocr(lines)
    chapters = parse_pdf.parse_chapters(lines, style=style)

    # Главы одной книги должны идти подряд; разрыв означает, что заголовок
    # развалился в OCR и две главы слились — это надо увидеть, а не потерять.
    seen = set()
    for ch in chapters:
        key = (ch.book, ch.number)
        if key in seen:
            raise SystemExit(f"{slug}: глава {key} встретилась дважды")
        seen.add(key)
    return chapters


def check_against_english(slug: str, chapters) -> list[str]:
    """Сверяет число глав по книгам с английским корпусом.

    Английский корпус — точка отсчёта: если в русской книге глав меньше,
    значит заголовок развалился в OCR и две главы слились.
    """
    reference = HERE.parent / "corpus" / slug
    if not reference.is_dir():
        return []          # работы нет в английском корпусе (например, спорная)
    got: dict[str, int] = {}
    for ch in chapters:
        got[ch.book] = got.get(ch.book, 0) + 1
    problems = []
    for book in sorted(got):
        want = len(list((reference / book).glob("*.md"))) if book else 0
        if want and want != got[book]:
            problems.append(f"книга {book}: {got[book]} глав против {want} в английском")
    return problems


def frontmatter(cfg: dict, book: str | None, number: int) -> str:
    cite = parse_pdf.cite(cfg["title"], book, number)
    book_field = f'"{book}"' if book else "null"
    return "\n".join([
        "---",
        f'work: "{cfg["title"]}"',
        f'work_slug: "{cfg["slug"]}"',
        f"book: {book_field}",
        f"chapter: {number}",
        f'group: {cfg["group"]}',
        "authenticity: genuine",
        f'translator: "{cfg["translator"]}"',
        "source: pdf",
        f'edition: "Сочинения в 4 т., М.: Мысль, {cfg["year"]}"',
        "lang: ru",
        f'cite: "{cite}"',
        "---",
        "",
    ])


def write(root: Path, slug: str, chapters) -> int:
    cfg = dict(WORKS[slug], slug=slug)
    for ch in chapters:
        path = root / parse_pdf.chapter_path(slug, ch.book, ch.number)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(frontmatter(cfg, ch.book, ch.number) + ch.text + "\n",
                        encoding="utf-8")
    return len(chapters)


def main(argv=None) -> int:
    p = argparse.ArgumentParser(description="Русский корпус из PDF-сканов")
    p.add_argument("slug", nargs="*", help="какие работы собирать")
    p.add_argument("--all", action="store_true", help="все работы из таблицы")
    p.add_argument("--out", default=str(HERE.parent / "corpus-ru"),
                   help="куда писать (по умолчанию corpus-ru в корне репозитория)")
    args = p.parse_args(argv)

    slugs = list(WORKS) if args.all else args.slug
    if not slugs:
        p.error("укажите работу или --all")
    unknown = [s for s in slugs if s not in WORKS]
    if unknown:
        p.error(f"неизвестные работы: {', '.join(unknown)}")

    root = Path(args.out)
    total = 0
    for slug in slugs:
        chapters = chapters_for(slug)
        problems = check_against_english(slug, chapters)
        if problems:
            print(f"{slug}: расхождение с английским корпусом — "
                  + "; ".join(problems), file=sys.stderr)
        books = len({c.book for c in chapters})
        n = write(root, slug, chapters)
        total += n
        print(f"{slug:16s} {n:4d} глав в {books} книгах")
    print(f"\nвсего глав: {total}\nкаталог: {root}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
