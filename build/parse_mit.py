import re
from dataclasses import dataclass

RULE_RE = re.compile(r"^-{10,}\s*$")
TRANSLATOR_RE = re.compile(r"^Translated by (.+?)\s*$", re.M)
BOOK_RE = re.compile(r"^BOOK\s+([IVXLC]+)\s*$")
SECTION_RE = re.compile(r"^SECTION\s+\d+\s*$")
PART_RE = re.compile(r"^Part\s+(\d+|[IVXLC]+)\s*(.*)$")
BARE_RE = re.compile(r"^(\d+)\s*$")
END_RE = re.compile(r"^THE END\s*$")

ROMAN = {"I": 1, "V": 5, "X": 10, "L": 50, "C": 100}


class ParseError(Exception):
    pass


@dataclass(frozen=True)
class Chapter:
    book: str | None
    number: int
    text: str


def roman_to_int(token: str) -> int:
    total = 0
    prev = 0
    for char in reversed(token.upper()):
        value = ROMAN[char]
        if value < prev:
            total -= value
        else:
            total += value
            prev = value
    return total


def _to_int(token: str) -> int:
    return int(token) if token.isdigit() else roman_to_int(token)


def _merge_stray_symbols(paragraphs: list[str]) -> list[str]:
    # Изредка исходник MIT содержит "абзац" из одного знака препинания —
    # осиротевшую кавычку на отдельной строке между двумя пустыми (не
    # только в строке-маркере главы, но и внутри самого тела текста —
    # например, в metaphysics она стоит отдельной строкой перед первым
    # абзацем каждой книги, начиная со второй). Приклеиваем такой
    # фрагмент к началу следующего абзаца, восстанавливая исходный текст,
    # а не оставляем самостоятельным "абзацем" из одного символа.
    merged: list[str] = []
    pending: str | None = None
    for paragraph in paragraphs:
        if pending is not None:
            paragraph = pending + " " + paragraph
            pending = None
        if len(paragraph.strip()) <= 1:
            pending = paragraph
            continue
        merged.append(paragraph)
    if pending is not None:
        merged.append(pending)
    return merged


def _reflow(lines: list[str]) -> str:
    paragraphs: list[str] = []
    buf: list[str] = []
    for line in lines:
        stripped = line.strip()
        if stripped:
            buf.append(stripped)
        elif buf:
            paragraphs.append(" ".join(buf))
            buf = []
    if buf:
        paragraphs.append(" ".join(buf))
    return "\n\n".join(_merge_stray_symbols(paragraphs))


def parse(raw: str) -> tuple[str | None, list[Chapter]]:
    lines = raw.splitlines()

    rule_at = next((i for i, l in enumerate(lines) if RULE_RE.match(l)), None)
    if rule_at is None:
        raise ParseError("не найден конец шапки: нет строки из дефисов")

    match = TRANSLATOR_RE.search("\n".join(lines[:rule_at]))
    translator = match.group(1) if match else None

    body = lines[rule_at + 1:]
    end_at = next((i for i, l in enumerate(body) if END_RE.match(l)), None)
    if end_at is not None:
        body = body[:end_at]

    # Стиль разметки определяется один раз на всю работу. Раньше хватало
    # одной строки, похожей на "Part N", чтобы переключить работу в
    # part-стиль — а значит одна случайная строка прозы вида "Part 3 ..."
    # молча гасила распознавание голых цифр во всей остальной работе.
    # Настоящая part-работа всегда содержит и первую, и вторую главу;
    # требуем, чтобы оба номера — 1 и 2 — нашлись среди строк, подходящих
    # под PART_RE (римские приведены к числам). Случайная строка прозы
    # почти никогда не даёт оба этих номера одновременно.
    part_numbers = {
        _to_int(m.group(1)) for m in (PART_RE.match(l) for l in body) if m
    }
    uses_parts = {1, 2} <= part_numbers

    def chapter_match(line: str) -> tuple[int, str] | None:
        # В части файлов (metaphysics, on-youth-and-old-age,
        # posterior-analytics) строка-маркер главы склеена с первым словом
        # или символом следующего абзаца ("Part 2 \"", "Part 15 Some animals
        # ..."). Разделитель "\s*" в PART_RE это учитывает: остаток строки
        # после номера — не часть маркера, а начало текста главы.
        match = PART_RE.match(line)
        if match:
            return _to_int(match.group(1)), match.group(2)
        if uses_parts:
            return None
        match = BARE_RE.match(line)
        return (int(match.group(1)), "") if match else None

    chapters: list[Chapter] = []
    book: str | None = None
    number: int | None = None
    buf: list[str] = []
    last = 0

    def flush() -> None:
        if number is not None:
            chapters.append(Chapter(book=book, number=number, text=_reflow(buf)))

    for line in body:
        # SECTION — артефакт постраничной разбивки сайта, а не деление текста:
        # нумерация глав идёт сквозь секции непрерывно.
        if RULE_RE.match(line) or SECTION_RE.match(line):
            continue
        book_match = BOOK_RE.match(line)
        if book_match:
            flush()
            book, number, buf, last = book_match.group(1), None, [], 0
            continue
        matched = chapter_match(line)
        if matched is not None and matched[0] == last + 1:
            candidate, leftover = matched
            flush()
            number = candidate
            last = candidate
            # Остаток склеенной строки-маркера кладём первой "строкой"
            # текста главы; _reflow (через _merge_stray_symbols) сам не
            # даст ему стать самостоятельным "абзацем" из одного символа,
            # если сразу за ним идёт пустая строка.
            buf = [leftover] if leftover else []
            continue
        if number is not None:
            buf.append(line)

    flush()
    if not chapters:
        raise ParseError("не найдено ни одной главы")
    return translator, chapters
