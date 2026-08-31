import re
from dataclasses import dataclass

RULE_RE = re.compile(r"^-{10,}\s*$")
TRANSLATOR_RE = re.compile(r"^Translated by (.+?)\s*$", re.M)
BOOK_RE = re.compile(r"^BOOK\s+([IVXLC]+)\s*$")
CHAPTER_RE = re.compile(r"^(\d+)\s*$")
END_RE = re.compile(r"^THE END\s*$")


class ParseError(Exception):
    pass


@dataclass(frozen=True)
class Chapter:
    book: str | None
    number: int
    text: str


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

    header = "\n".join(lines[:rule_at])
    m = TRANSLATOR_RE.search(header)
    translator = m.group(1) if m else None

    body = lines[rule_at + 1:]
    end_at = next((i for i, l in enumerate(body) if END_RE.match(l)), None)
    if end_at is not None:
        body = body[:end_at]

    chapters: list[Chapter] = []
    book: str | None = None
    number: int | None = None
    buf: list[str] = []
    last_number = 0

    def flush() -> None:
        if number is not None:
            chapters.append(Chapter(book=book, number=number, text=_reflow(buf)))

    for line in body:
        if RULE_RE.match(line):
            continue
        bm = BOOK_RE.match(line)
        if bm:
            flush()
            book, number, buf, last_number = bm.group(1), None, [], 0
            continue
        cm = CHAPTER_RE.match(line)
        if cm and int(cm.group(1)) == last_number + 1:
            flush()
            number = int(cm.group(1))
            last_number = number
            buf = []
            continue
        if number is not None:
            buf.append(line)

    flush()
    if not chapters:
        raise ParseError("не найдено ни одной главы")
    return translator, chapters
