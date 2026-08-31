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
    return "\n\n".join(paragraphs)


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

    # Стиль разметки определяется один раз на всю работу: если есть хоть одна
    # строка "Part N", голые цифры в тексте главами не считаются.
    uses_parts = any(PART_RE.match(l) for l in body)

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
            buf = [leftover] if leftover else []
            continue
        if number is not None:
            buf.append(line)

    flush()
    if not chapters:
        raise ParseError("не найдено ни одной главы")
    return translator, chapters
