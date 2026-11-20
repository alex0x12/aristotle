"""Импорт работ Аристотеля вне четырёхтомника в corpus-ru.

Источники — mobi/djvu/pdf из ``~/Downloads/Aristotel``. Текст конвертируется
один раз в ``build/.cache/extra`` (ebook-convert для mobi/djvu, pdftotext для
pdf), затем режется на работы/книги/главы по разметке конкретного издания.

Запуск:
    venv/bin/python import_extra.py --all
    venv/bin/python import_extra.py --only athenian-constitution
"""
import argparse
import re
import shutil
import subprocess
import sys
from pathlib import Path

import parse_pdf

HERE = Path(__file__).parent
CACHE = HERE / ".cache" / "extra"
DOWNLOADS = Path.home() / "Downloads" / "Aristotel"
OUT = HERE.parent / "corpus-ru"

EXTRA = {
    "athenian-constitution": dict(
        file="_aristotel-afinskaya_politiya-1506257386.mobi", kind="mobi",
        text="athenian-constitution.txt", parser="athenian",
        title="Афинская полития", translator="С. И. Радциг", year="1997",
        edition="Аристотель. Политика. Афинская полития. М.: Мысль, 1997",
        group="politics",
    ),
    "history-of-animals": dict(
        file="_aristotel-istoriya_zhivotnih-65bc1c042e5c7.mobi", kind="mobi",
        text="history-of-animals.txt", parser="history",
        title="История животных", translator="В. П. Карпов", year="1996",
        edition="Аристотель. История животных. М., 1996",
        group="biology",
    ),
    "on-sense-and-the-sensible": dict(
        file="Аристотель. Протрептик. О чувственном восприятии. О памяти.pdf",
        kind="pdf", text="protreptic.txt", parser="sense",
        title="О чувственном восприятии", translator="Е. В. Алымова",
        year="2004",
        edition="Аристотель. Протрептик. О чувственном восприятии. О памяти. СПб.: Изд-во СПбГУ, 2004",
        group="psychology",
    ),
    "on-memory": dict(
        file="Аристотель. Протрептик. О чувственном восприятии. О памяти.pdf",
        kind="pdf", text="protreptic.txt", parser="memory",
        title="О памяти", translator="Е. В. Алымова", year="2004",
        edition="Аристотель. Протрептик. О чувственном восприятии. О памяти. СПб.: Изд-во СПбГУ, 2004",
        group="psychology",
    ),
    "on-dreams": dict(
        file="О сновидениях.pdf", kind="pdf", text="on-dreams.txt",
        parser="dreams",
        title="О сновидениях", translator="С. В. Месяц", year="2024",
        edition="Платоновские исследования 21.2 (2024)",
        group="psychology",
    ),
    # В изданиях 1937/1940 гг. есть только книги, глав нет вообще; собираем
    # по одному файлу на книгу.
    "parts-of-animals": dict(
        file="o_chastyah_zhivotnih.djvu", kind="djvu",
        text="parts-of-animals.txt", parser="parts_books",
        title="О частях животных", translator="В. П. Карпов", year="1937",
        edition="Аристотель. О частях животных. М.: Биомедгиз, 1937",
        group="biology", book_only=True,
    ),
    "generation-of-animals": dict(
        file="o_vozniknovenii_zhivotnih.djvu", kind="djvu",
        text="generation-of-animals.txt", parser="gen_books",
        title="О возникновении животных", translator="В. П. Карпов",
        year="1940",
        edition="Аристотель. О возникновении животных. М.; Л., 1940",
        group="biology", book_only=True,
    ),
}


def ensure_text(slug: str) -> list[str]:
    """Строки источника; конвертация кешируется в build/.cache/extra."""
    cfg = EXTRA[slug]
    cache = CACHE / cfg["text"]
    if not cache.exists():
        src = DOWNLOADS / cfg["file"]
        if not src.exists():
            raise SystemExit(f"нет файла: {src}")
        CACHE.mkdir(parents=True, exist_ok=True)
        if cfg["kind"] in ("mobi", "djvu"):
            subprocess.run(["ebook-convert", str(src), str(cache)],
                           check=True, stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL)
        else:
            subprocess.run(["pdftotext", "-layout", str(src), str(cache)],
                           check=True)
    return cache.read_text(encoding="utf-8").splitlines()


# --- Афинская полития ------------------------------------------------------

ATHENIAN_SECTION_RE = re.compile(r"^\s*(\d{1,3})\s*[.…]\s+(\S.*)$")
ATHENIAN_HEADING_RE = re.compile(r"^\s*[IVXLC]+\.\s+[А-ЯЁ]")
PAGE_NUMBER_RE = re.compile(r"^\s*\d{1,4}\s*$")
RUNNING_HEAD_RE = re.compile(
    r"^\s*(?:\d+\s+)?(?:О чувственном восприятии|О памяти|Аристотель|"
    r"Платоновские исследования|Светлана Месяц|История животных|"
    r"Афинская полития)(?:\s+\d+)?\s*$")


def _service_line(line: str) -> bool:
    """Номер страницы или колонтитул — в текст главы не берём."""
    stripped = line.strip()
    return bool(
        PAGE_NUMBER_RE.match(line)
        or RUNNING_HEAD_RE.match(line)
        or stripped.startswith("Светлана Месяц")
        or "Платоновские исследования" in stripped
    )


def parse_athenian(lines: list[str]) -> list[parse_pdf.Chapter]:
    """Разделы 1–69 идут подряд и совпадают с главами английского корпуса."""
    starts = [i for i, line in enumerate(lines)
              if line.strip().startswith("ЧАСТЬ ПЕРВАЯ")]
    if not starts:
        raise ValueError("не найдено начало тела «Афинской политии»")
    chapters: list[parse_pdf.Chapter] = []
    number = None
    body: list[str] = []

    def flush():
        if number is not None:
            chapters.append(parse_pdf.Chapter(
                book=None, number=number, text="\n".join(body).strip()))

    for line in lines[starts[-1]:]:
        m = ATHENIAN_SECTION_RE.match(line)
        if m:
            flush()
            number, body = int(m.group(1)), [m.group(2).strip()]
        elif (number is not None and line.strip()
              and not ATHENIAN_HEADING_RE.match(line)
              and not line.strip().startswith("ЧАСТЬ")
              and not _service_line(line)):
            body.append(line.strip())
    flush()
    return [chapter for chapter in chapters if chapter.text]


# --- История животных ------------------------------------------------------

HA_BOOK_RE = re.compile(
    r"^Книга\s+(первая|вторая|третья|четвертая|пятая|шестая|седьмая|"
    r"восьмая|девятая|десятая)\s*$", re.IGNORECASE)
HA_BOOK_NO = {
    "первая": "i", "вторая": "ii", "третья": "iii", "четвертая": "iv",
    "пятая": "v", "шестая": "vi", "седьмая": "vii", "восьмая": "viii",
    "девятая": "ix", "десятая": "x",
}
HA_CHAPTER_RE = re.compile(
    r"^Г[ЛA]АВА\s+([0-9IVXLCivxlcХхΧχІі]+)\s*$", re.IGNORECASE)
HA_STOP = ("ПРИМЕЧАНИЯ", "УКАЗАТЕЛЬ")


def _chapter_number(raw: str) -> int | None:
    if raw.isdigit():
        return int(raw)
    return parse_pdf.roman_value(raw)


def parse_history(lines: list[str]) -> list[parse_pdf.Chapter]:
    """Книги и главы «Истории животных».

    В издании девять книг и десятая («спорная»); десятую не берём, чтобы
    структура совпадала с английским корпусом (там её нет). Главы нумеруются
    так, как напечатаны: в книгах VIII–IX номера с пропусками — это деление
    самого издания, а не потеря текста.
    """
    body_start = next((i for i, line in enumerate(lines)
                       if HA_BOOK_RE.match(line.strip())
                       and HA_BOOK_RE.match(line.strip()).group(1).lower()
                       == "первая"), None)
    if body_start is None:
        raise ValueError("не найдено начало «Истории животных»")

    chapters: list[parse_pdf.Chapter] = []
    book = None
    number = None
    body: list[str] = []

    def flush():
        if book is not None and number is not None:
            chapters.append(parse_pdf.Chapter(
                book=book, number=number, text="\n".join(body).strip()))

    for line in lines[body_start:]:
        s = line.strip()
        if s in HA_STOP:
            break
        m_book = HA_BOOK_RE.match(s)
        if m_book:
            flush()
            book = HA_BOOK_NO[m_book.group(1).lower()]
            number, body = None, []
            continue
        m_ch = HA_CHAPTER_RE.match(s)
        if m_ch:
            value = _chapter_number(m_ch.group(1))
            if value is not None:
                flush()
                number, body = value, []
                continue
        if (book is not None and number is not None and s
                and not _service_line(s)):
            body.append(s)
    flush()
    return [chapter for chapter in chapters
            if chapter.text and chapter.book != "x"]


# --- «О чувственном восприятии» и «О памяти» -------------------------------

ROMAN_SECTION_RE = re.compile(r"^\s*([IVXLC]+)\s*$")


def _roman_sections(lines: list[str], start: int, end: int,
                    want: int) -> list[tuple[int, int]]:
    """Индексы строк с римскими разделами I..`want` (в возрастающем порядке)."""
    found: list[tuple[int, int]] = []
    expected = 1
    for i in range(start, end):
        m = ROMAN_SECTION_RE.match(lines[i])
        if not m:
            continue
        value = parse_pdf.roman_value(m.group(1))
        if value == expected:
            found.append((i, value))
            expected += 1
            if expected > want:
                break
    if len(found) != want:
        raise ValueError(f"найдено разделов {len(found)}, ожидалось {want}")
    return found


def _split_by_sections(lines: list[str], start: int, end: int,
                       markers: list[tuple[int, int]]) -> list[parse_pdf.Chapter]:
    chapters: list[parse_pdf.Chapter] = []
    for pos, (idx, value) in enumerate(markers):
        stop = markers[pos + 1][0] if pos + 1 < len(markers) else end
        text = "\n".join(line.strip() for line in lines[idx + 1:stop]
                         if line.strip() and not _service_line(line))
        if pos == 0:
            before = "\n".join(line.strip() for line in lines[start:idx]
                               if line.strip() and not _service_line(line))
            text = (before + "\n" + text).strip()
        if text:
            chapters.append(parse_pdf.Chapter(
                book=None, number=value, text=text))
    return chapters


def parse_sense(lines: list[str]) -> list[parse_pdf.Chapter]:
    """«О чувственном восприятии»: семь римских разделов в переводе 2004 г."""
    opening = next((i for i, line in enumerate(lines)
                    if "После того как о душе самой по себе" in line), None)
    if opening is None:
        raise ValueError("не найдено начало «О чувственном восприятии»")
    # Маркер I стоит непосредственно перед вступительной фразой; если его
    # нет (например, в фикстуре), берём саму фразу.
    before = [
        i for i in range(max(0, opening - 10), opening + 1)
        if ROMAN_SECTION_RE.match(lines[i])
        and parse_pdf.roman_value(
            ROMAN_SECTION_RE.match(lines[i]).group(1)) == 1]
    start = max(before) if before else opening
    # «О памяти» начинается со второго раздела I после начала восприятия.
    ones = [
        i for i in range(start, len(lines))
        if ROMAN_SECTION_RE.match(lines[i])
        and parse_pdf.roman_value(
            ROMAN_SECTION_RE.match(lines[i]).group(1)) == 1]
    memory_start = ones[1] if len(ones) > 1 else len(lines)
    markers = _roman_sections(lines, start, memory_start, 7)
    return _split_by_sections(lines, start, memory_start, markers)


def parse_memory(lines: list[str]) -> list[parse_pdf.Chapter]:
    """«О памяти»: два римских раздела, после них — комментарий."""
    opening = next(i for i, line in enumerate(lines)
                   if "Речь пойдет о том, что есть, по какой" in line)
    comment = next(i for i in range(opening, len(lines))
                   if lines[i].strip().startswith("КОММЕНТАРИЙ"))
    markers = _roman_sections(lines, opening - 3, comment, 2)
    return _split_by_sections(lines, opening - 3, comment, markers)


# --- «О сновидениях» -------------------------------------------------------

DREAMS_MARKER_RE = re.compile(r"^\s*([123])\.\s+[А-ЯЁ]")


def parse_dreams(lines: list[str]) -> list[parse_pdf.Chapter]:
    """Три главы перевода 2024 г.; комментарии и литература отбрасываются."""
    start = next((i for i, line in enumerate(lines)
                  if re.match(r"^\s*1\.\s+Теперь", line)), None)
    if start is None:
        raise ValueError("не найдено начало «О сновидениях»")
    stop = next((i for i in range(start, len(lines))
                 if lines[i].strip().startswith("Литература")), len(lines))
    chapters: list[parse_pdf.Chapter] = []
    number = None
    body: list[str] = []

    def flush():
        if number is not None and body:
            chapters.append(parse_pdf.Chapter(
                book=None, number=number, text="\n".join(body).strip()))

    for line in lines[start:stop]:
        m = DREAMS_MARKER_RE.match(line)
        if m and int(m.group(1)) == (number or 0) + 1:
            flush()
            number = int(m.group(1))
            body = [line.strip()]
        elif (number is not None and line.strip()
              and not _service_line(line)):
            body.append(line.strip())
    flush()
    return chapters


# --- книги без глав (djvu 1937/1940) ---------------------------------------

BOOK_LINE_RE = re.compile(
    r"^КНИГА[Г\s]*(ПЕРВАЯ|ВТОРАЯ|ТРЕТЬЯ|ЧЕТВЕРТАЯ|ПЯТАЯ)\b", re.IGNORECASE)
BOOK_ROMAN = {
    "ПЕРВАЯ": "i", "ВТОРАЯ": "ii", "ТРЕТЬЯ": "iii",
    "ЧЕТВЕРТАЯ": "iv", "ПЯТАЯ": "v",
}
BOOK_RUNNING_RE = re.compile(
    r"^(?:[Оо]\s+ЧАСТЯХ[\s-]*[ЖЩ][Ии]ВОТНЫХ|Аристотель|"
    r"[Оо]\s+\.?\s*ВОЗНИКНОВЕНИИ\s+[Жж]ивотных|"
    r"ВОЗНИКНОВЕНИИ\s+[Жж]ивотных|"
    r"В\.?\s*[ПТ]\.?\s*КАРПОВ|АРИСТОТЕЛЬ\s+И\s+АНТИЧНАЯ\s+\S+|"
    r"ОТ\s+ИЗДАТЕЛЬСТВА|ИЗДАТЕЛЬСТВА)[\s.,:—-]*", re.IGNORECASE)
BOOK_STOP_RE = re.compile(
    r"(?:\bПРИМЕЧАНИЯ\b|\bПРИЛОЖЕНИЕ\b|\bУКАЗАТЕЛЬ\b|"
    r"Перевод\s+сделан\s+с\s+греческого)")


def _strip_page_prefix(line: str) -> str:
    """Убирает с начала строки колонтитул и номер страницы."""
    text = line.strip()
    # Номер страницы иногда прочитан как греческая буква или знак («Θ …»).
    text = re.sub(r"^[ΘÔΌθ.·]{1,2}\s*", "", text)
    # «КНИГА ПЕРВАЯ (А) 1 …», «КНИГА ЧЕТВЕРТАЯ А 1 …»: заголовок книги,
    # затем номер страницы (иногда прочитанный как 1–3 заглавные буквы).
    text = re.sub(r"^КНИГА[^(]{0,25}(?:\([^)]{1,5}\))?\s*", "", text,
                  flags=re.IGNORECASE)
    text = re.sub(r"^\d{1,3}[A-Za-z]?\s+", "", text)
    # Колонтитул снимаем до коротких «номеров страниц»: иначе «О» из
    # «О ВОЗНИКНОВЕНИИ…» будет принято за номер.
    text = BOOK_RUNNING_RE.sub("", text)
    text = re.sub(r"^[A-Za-z]\s+(?=[Оо]\s+ЧАСТЯХ)", "", text)
    text = re.sub(r"^(?:\d{1,3}|[А-ЯЁ]{2,3})\s+", "", text)
    text = re.sub(r"^\d{1,3}[^\wА-Яа-я]{0,3}\s*", "", text)
    text = BOOK_RUNNING_RE.sub("", text)
    text = re.sub(r"^\d{1,3}[^\wА-Яа-я]{0,3}\s*", "", text)
    text = re.sub(r"^[^\s]*\d[^\s]*\s+", "", text)
    return text.strip()


def _clean_book_text(text: str) -> str:
    """Снимает колонтитулы, приклеившиеся в конце строки при конвертации."""
    cleaned = []
    for line in text.split("\n"):
        line = re.sub(
            r"\s+(?:Аристотель|ВОЗНИКНОВЕНИИ\s+[Жж]ивотных|"
            r"В\.\s*[ПТ]\.\s*КАРПОВ)\s*(?:\d{1,3}[А-ЯA-Za-z]?)?\s*$",
            "", line)
        line = re.sub(
            r"\s+[Оо]\s+ЧАСТЯХ[\s-]*[ЖЩ][Ии]ВОТНЫХ\s+\d{1,3}(?=\s|$)",
            " ", line)
        cleaned.append(line)
    return re.sub(r"[ \t]{2,}", " ", "\n".join(cleaned)).strip()


def _parse_book_level(lines: list[str]) -> list[parse_pdf.Chapter]:
    start = next((i for i, line in enumerate(lines)
                  if BOOK_LINE_RE.match(line.strip())), None)
    if start is None:
        raise ValueError("не найдено начало книги")
    book = None
    texts: dict[str, list[str]] = {}
    for line in lines[start:]:
        stripped = line.strip()
        stop = BOOK_STOP_RE.search(stripped)
        if stop:
            before = _strip_page_prefix(stripped[:stop.start()])
            if before and book:
                texts[book].append(before)
            break
        match = BOOK_LINE_RE.match(stripped)
        if match:
            found = BOOK_ROMAN[match.group(1).upper()]
            if found != book:
                book = found
                texts[book] = []
            rest = _strip_page_prefix(stripped)
            if rest:
                texts[book].append(rest)
            continue
        if book and stripped:
            rest = _strip_page_prefix(stripped)
            if rest and not _service_line(rest):
                texts[book].append(rest)
    return [parse_pdf.Chapter(book=key, number=1,
                              text=_clean_book_text("\n".join(value)))
            for key, value in texts.items() if value]


def parse_parts_books(lines: list[str]) -> list[parse_pdf.Chapter]:
    return _parse_book_level(lines)


def parse_gen_books(lines: list[str]) -> list[parse_pdf.Chapter]:
    return _parse_book_level(lines)


PARSERS = {
    "athenian": parse_athenian,
    "history": parse_history,
    "sense": parse_sense,
    "memory": parse_memory,
    "dreams": parse_dreams,
    "parts_books": parse_parts_books,
    "gen_books": parse_gen_books,
}


def frontmatter(slug: str, chapter: parse_pdf.Chapter) -> str:
    cfg = EXTRA[slug]
    if cfg.get("book_only") and chapter.book:
        cite = f'{cfg["title"]} {chapter.book.upper()}'
    else:
        cite = parse_pdf.cite(cfg["title"], chapter.book, chapter.number)
    book = f'"{chapter.book}"' if chapter.book else "null"
    return "\n".join([
        "---",
        f'work: "{cfg["title"]}"',
        f'work_slug: "{slug}"',
        f"book: {book}",
        f"chapter: {chapter.number}",
        f'group: {cfg["group"]}',
        "authenticity: genuine",
        f'translator: "{cfg["translator"]}"',
        "source: file",
        f'edition: "{cfg["edition"]}"',
        "lang: ru",
        f'cite: "{cite}"',
        "---",
        "",
    ])


def write_work(slug: str, chapters: list[parse_pdf.Chapter], out: Path) -> int:
    target = out / slug
    if target.exists():
        shutil.rmtree(target)
    for chapter in chapters:
        path = out / parse_pdf.chapter_path(slug, chapter.book, chapter.number)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(frontmatter(slug, chapter) + chapter.text + "\n",
                        encoding="utf-8")
    return len(chapters)


def build(slug: str, out: Path) -> list[parse_pdf.Chapter]:
    cfg = EXTRA[slug]
    lines = ensure_text(slug)
    chapters = PARSERS[cfg["parser"]](lines)
    if cfg.get("book_only"):
        print(f"{slug}: в источнике нет глав, по одному файлу на книгу",
              file=sys.stderr)
    else:
        for problem in check_english(slug, chapters):
            print(f"{slug}: {problem}", file=sys.stderr)
    n = write_work(slug, chapters, out)
    books = len({c.book for c in chapters if c.book})
    where = f"в {books} книгах" if books else "без книг"
    print(f"{slug:28s} {n:4d} глав {where}")
    return chapters


def check_english(slug: str, chapters) -> list[str]:
    reference = HERE.parent / "corpus" / slug
    if not reference.is_dir():
        return []
    got: dict[str | None, int] = {}
    for chapter in chapters:
        got[chapter.book] = got.get(chapter.book, 0) + 1
    problems = []
    for book, count in sorted(got.items(), key=lambda item: str(item[0])):
        if book is None:
            want = len(list((reference).glob("*.md")))
        else:
            want = len(list((reference / book).glob("*.md")))
        if want and want != count:
            problems.append(f"книга {book or '—'}: {count} глав против {want}")
    return problems


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description="Импорт работ вне четырёхтомника")
    parser.add_argument("--only", nargs="*", default=[],
                        help="слаги (по умолчанию все реализованные)")
    parser.add_argument("--all", action="store_true", help="все реализованные")
    parser.add_argument("--out", default=str(OUT))
    args = parser.parse_args(argv)

    slugs = args.only or list(EXTRA)
    unknown = [s for s in slugs if s not in EXTRA]
    if unknown:
        parser.error(f"неизвестные работы: {', '.join(unknown)}")
    out = Path(args.out)
    total = 0
    for slug in slugs:
        total += len(build(slug, out))
    print(f"\nвсего глав: {total}\nкаталог: {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
