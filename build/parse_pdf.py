"""Разбор сканов «Философского наследия»: слова со страницы -> главы.

Источник — PDF с текстовым слоем ABBYY FineReader и беккеровскими номерами в
поле. Номера стоят вне набора: на чётных страницах слева, на нечётных справа,
поэтому отсекаются по координатам, а не угадываются по виду.
"""
import re
from dataclasses import dataclass
from pathlib import Path

# Края, за которыми стоят беккеровские номера. Это НЕ границы набора: тело на
# разных страницах начинается то с x≈33, то с x≈51, поэтому резать текст по
# постоянному полю нельзя. За краем оказывается только номер, и признак у него
# свой — он один в строке и состоит из цифр или латиницы.
EDGE_LEFT = 45.0
EDGE_RIGHT = 40.0

# Порядковые номера книг и глав в издании.
ORDINALS = {
    "ПЕРВАЯ": 1, "ВТОРАЯ": 2, "ТРЕТЬЯ": 3, "ЧЕТВЕРТАЯ": 4, "ПЯТАЯ": 5,
    "ШЕСТАЯ": 6, "СЕДЬМАЯ": 7, "ВОСЬМАЯ": 8, "ДЕВЯТАЯ": 9, "ДЕСЯТАЯ": 10,
    "ОДИННАДЦАТАЯ": 11, "ДВЕНАДЦАТАЯ": 12, "ТРИНАДЦАТАЯ": 13,
    "ЧЕТЫРНАДЦАТАЯ": 14, "ПЯТНАДЦАТАЯ": 15, "ШЕСТНАДЦАТАЯ": 16,
    "СЕМНАДЦАТАЯ": 17, "ВОСЕМНАДЦАТАЯ": 18, "ДЕВЯТНАДЦАТАЯ": 19,
    "ДВАДЦАТАЯ": 20, "ТРИДЦАТАЯ": 30, "СОРОКОВАЯ": 40, "ПЯТИДЕСЯТАЯ": 50,
}

TENS = {"ДВАДЦАТЬ": 20, "ТРИДЦАТЬ": 30, "СОРОК": 40, "ПЯТЬДЕСЯТ": 50}

# OCR путает «КНИГА» с «КИНГА» и «КПИГА» (том 3): буквы Н/П/И переставляются.
# Без распознавания книги не сбрасывается нумерация глав.
BOOK_RE = re.compile(r"^К[А-ЯЁ]{2,3}ГА\s+([А-ЯЁ]+)\b")
# Главы — то порядковым словом («ГЛАВА ПЕРВАЯ»), то цифрой («ГЛАВА 46»):
# в «Аналитиках» глав десятки. OCR иногда теряет «А» в слове ГЛАВА
# («ГЛЛВА ПЕРВАЯ») или приклеивает кавычку после заголовка.
CHAPTER_RE = re.compile(
    r"^ГЛ[А-Я]?ВА\s+(\d{1,3}|[А-ЯЁ]+(?:\s+[А-ЯЁ]+)?)\s*[«»\"']*\s*$")

# Второй стиль — тома, где глава начинается номером с точкой и сразу текстом:
# «1. Собираясь говорить о вопросах этики...».
# Номер может прийти кириллицей: OCR читает «3» как «З», «11» как «И».
NUMBERED_RE = re.compile(r"^([0-9ЗзОоИиIl]{1,3})[.·]\s*(\S.*)$")

# Кириллица и латиница, которыми OCR заменяет цифры в номере главы.
DIGIT_LOOKALIKE = {"З": "3", "з": "3", "О": "0", "о": "0",
                   "И": "11", "и": "11", "I": "1", "l": "1"}

# Слово, похожее на беккеровский номер: «35», «980а», «»8ot>», «IV».
BEKKER_TOKEN_RE = re.compile(
    r"^[«»<>]?(\d{1,4}|[IVXLCDMivxlcdm]{1,4}|[a-z]{1,3})[а-яa-z]?[<>»]?$")

# Остатки беккеровских номеров, попавшие в поток текста: «980а», «»8ot>».
# Хвост из букв — след OCR: «8ot>» — это «80b». Больше четырёх букв не берём,
# чтобы правило не съедало слова, начинающиеся с цифры.
BEKKER_RE = re.compile(r"(?:^|\s)[»<]?\d{1,4}[а-яa-zA-Zα-ω]{0,4}[>»]?(?=\s|$)")

ROMAN = ["i", "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x",
         "xi", "xii", "xiii", "xiv", "xv", "xvi", "xvii", "xviii", "xix", "xx"]


class HeadingError(Exception):
    """Заголовок нашёлся, но порядковое слово неизвестно — значит OCR его порвал."""


@dataclass(frozen=True)
class Word:
    x0: float
    y0: float
    x1: float
    y1: float
    text: str


@dataclass
class Chapter:
    book: str | None
    number: int
    text: str


def page_lines(words, width: float) -> list[str]:
    """Строки страницы без полей, в порядке чтения.

    Слова поля отбрасываются: слева — если начинаются левее MARGIN_LEFT,
    справа — если кончаются правее width - MARGIN_RIGHT. Слова одной строки
    собираются по y с допуском: ABBYY даёт разные y для букв с выносными.
    """
    rows: list[list[Word]] = []
    for w in sorted(words, key=lambda w: (w.y0, w.x0)):
        if rows and abs(rows[-1][0].y0 - w.y0) <= 2.0:
            rows[-1].append(w)
        else:
            rows.append([w])
    out: list[str] = []
    for row in rows:
        if len(row) == 1 and is_margin_number(row[0], width):
            continue
        out.append(" ".join(x.text for x in sorted(row, key=lambda w: w.x0)).strip())
    return [line for line in out if line]


def is_margin_number(w: Word, width: float) -> bool:
    """Номер в поле: один в строке, похож на число и вынесен за край набора."""
    return bool(BEKKER_TOKEN_RE.match(w.text.strip())) and (
        w.x0 < EDGE_LEFT or w.x1 > width - EDGE_RIGHT)


def join_hyphenated(lines: list[str]) -> list[str]:
    """Склеивает слова, перенесённые на следующую строку.

    Признак переноса — дефис в конце строки и строчная буква в начале
    следующей. Тире (—) и заглавная буква означают, что переноса нет.
    """
    out: list[str] = []
    for line in lines:
        prev = out[-1] if out else None
        if prev is not None and prev.endswith(("-", "\u2010", "\u00ad")) \
                and line[:1].islower():
            out[-1] = prev[:-1] + line
        else:
            out.append(line)
    return out


# Колонтитул беккера: «1181 п24», «980а», «»8ot>». Смесь цифр и латиницы
# (иногда одной кириллической буквы) — по ней и отличается от слова.
# Токен с цифрой внутри (беккеровский номер): «1258ь», «Ю94а», «iU7n8».
# Регистр и порядок букв OCR путает, поэтому проверяем только состав и цифру.
HEADER_TOKEN_RE = re.compile(
    r"^[«»<>]?(?=[^\s]*\d)[0-9A-Za-zА-Яа-яЁёα-ω<>»]{1,8}[<>»]?$")


def strip_bekker(line: str) -> str:
    """Убирает беккеровские номера: колонтитул в начале и остатки в строке.

    Колонтитул может стоять перед номером главы («1181 п24 1. Собираясь…»),
    и без его снятия номер главы перестаёт распознаваться.
    """
    tokens = line.split()
    dropped = 0
    while tokens and dropped < 3 and HEADER_TOKEN_RE.match(tokens[0]):
        tokens.pop(0)
        dropped += 1
    return BEKKER_RE.sub("", " ".join(tokens)).strip()


def clean_lines(lines: list[str]) -> list[str]:
    """Порядок важен: сперва снять номера, потом склеивать переносы.

    Номер в конце строки («Доказа- 980а») прячет дефис от склейки.
    """
    return join_hyphenated([strip_bekker(line) for line in lines])


def ordinal_to_number(word: str) -> int | None:
    """«ПЕРВАЯ» -> 1, «ДВАДЦАТЬ ПЕРВАЯ» -> 21. Не разобрали — None."""
    parts = word.upper().split()
    if len(parts) == 1:
        return ORDINALS.get(parts[0])
    if len(parts) == 2 and parts[0] in TENS and parts[1] in ORDINALS:
        unit = ORDINALS[parts[1]]
        if 1 <= unit <= 9:
            return TENS[parts[0]] + unit
    return None


# Римские числа, как их видит OCR: I/l/1/|, кириллические Х/Ш/П, латиница.
ROMAN_LOOKALIKE = {
    "1": "I", "l": "I", "|": "I", "ΐ": "I", "ί": "I", "ι": "I",
    "Ι": "I", "І": "I", "i": "I", "х": "X", "Х": "X", "x": "X",
    "χ": "X", "Χ": "X",
    "ш": "III", "Ш": "III", "п": "II", "П": "II",
}
ROMAN_VALUES = {"I": 1, "V": 5, "X": 10, "L": 50, "C": 100}


def roman_value(raw: str) -> int | None:
    """Значение римского числа с поправкой на OCR-двойники."""
    letters = "".join(ROMAN_LOOKALIKE.get(ch, ch.upper())
                      for ch in raw if not ch.isspace())
    total = prev = 0
    for ch in reversed(letters):
        value = ROMAN_VALUES.get(ch)
        if value is None:
            return None
        total += -value if value < prev else value
        prev = max(prev, value)
    return total or None


# Маркер главы НЭ: римское число в скобках, закрывающая скобка может
# потеряться («2(Iiy.»). Арабское число перед ним — счётчик абзацев.
NE_MARKER_RE = re.compile(r"\(([\s0-9IlХхXШшVLC1ПпiΐίιΙІ|]{1,20})")
NE_ARABIC_RE = re.compile(r"^(\d{1,3})\s*[.·]\s+(.*)$")
NE_LEAD_ARABIC_RE = re.compile(r"^(\d{1,3})\b")
NE_MARKER_CHARS = set("() .·,\\0123456789IlХхXШшVLCПпiΐίιΙІ|")


def _ne_rest(line: str, start: int) -> str:
    """Текст строки после маркера главы."""
    close = line.find(")", start)
    if close != -1:
        i = close + 1
    else:
        i = start
        while i < len(line) and line[i] in NE_MARKER_CHARS:
            i += 1
    while i < len(line) and line[i] in " .·,\\-":
        i += 1
    return line[i:].strip()


def _parse_ne(lines: list[str]) -> list[Chapter]:
    """НЭ: глава — римское число в скобках, номер книги — «КНИГА …»."""
    book: str | None = None
    book_no = 0
    markers: list[dict] = []
    last_value: dict[str | None, int] = {}
    for idx, line in enumerate(lines):
        m_book = BOOK_RE.match(line)
        if m_book:
            book_no += 1
            book = ROMAN[book_no - 1] if book_no <= len(ROMAN) else str(book_no)
            last_value[book] = 0
            continue
        m = NE_MARKER_RE.search(line[:60])
        if not m:
            continue
        # Закрывающая скобка могла потеряться, но после маркера обязан идти
        # знак препинания; иначе это обычное слово в скобках («(Хвастуном…»).
        tail = line[m.end():m.end() + 4]
        if ")" not in tail and not any(ch in tail for ch in ".,·\\"):
            continue
        value = roman_value(m.group(1))
        if not value or not 1 <= value <= 30:
            continue
        previous = last_value.get(book, 0)
        if value != previous + 1:
            # Пропуск допустим (маркер потерялся), но назад — это уже не
            # заголовок, а скобка в тексте.
            if value <= previous:
                continue
        arabic = NE_LEAD_ARABIC_RE.match(line)
        markers.append(dict(
            idx=idx, book=book, value=value, rest=_ne_rest(line, m.start()),
            arabic=int(arabic.group(1)) if arabic else None,
            filled=False))
        last_value[book] = value

    markers = _fill_ne_gaps(lines, markers)
    return _ne_segments(lines, markers)


def _fill_ne_gaps(lines: list[str], markers: list[dict]) -> list[dict]:
    """Достраивает потерянные OCR маркеры глав по номерам абзацев.

    В издании первая строка главы начинается с номера абзаца; если римский
    маркер не распознан, этот номер обычно остаётся и позволяет найти границу.
    """
    per_book: dict[str | None, list[dict]] = {}
    for marker in markers:
        per_book.setdefault(marker["book"], []).append(marker)

    for book, items in per_book.items():
        items.sort(key=lambda m: m["idx"])
        used = {m["idx"] for m in items}
        filled: list[dict] = []
        for pos, current in enumerate(items):
            nxt = items[pos + 1] if pos + 1 < len(items) else None
            if nxt:
                end_idx = nxt["idx"]
                last_missing = nxt["value"] - 1
            else:
                end_idx = len(lines)
                for j in range(current["idx"] + 1, len(lines)):
                    if BOOK_RE.match(lines[j]):
                        end_idx = j
                        break
                # После последнего маркера пробуем до пяти глав подряд;
                # первая ненайденная обрывает поиск.
                last_missing = current["value"] + 5
            for missing in range(current["value"] + 1, last_missing + 1):
                base = current["arabic"]
                targets = []
                if base is not None:
                    targets.append(base + (missing - current["value"]))
                targets.append(missing)
                found = None
                for target in targets:
                    for i in range(current["idx"] + 1, end_idx):
                        if i in used:
                            continue
                        m = NE_ARABIC_RE.match(lines[i])
                        if m and int(m.group(1)) == target:
                            found = dict(idx=i, book=book, value=missing,
                                         rest=m.group(2).strip(),
                                         arabic=target, filled=True)
                            break
                    if found:
                        break
                if found:
                    used.add(found["idx"])
                    filled.append(found)
                elif not nxt:
                    break
        items.extend(filled)
        items.sort(key=lambda m: m["idx"])
    result = [marker for items in per_book.values() for marker in items]
    return sorted(result, key=lambda m: m["idx"])


def _ne_segments(lines: list[str], markers: list[dict]) -> list[Chapter]:
    chapters: list[Chapter] = []
    for i, marker in enumerate(markers):
        end = markers[i + 1]["idx"] if i + 1 < len(markers) else len(lines)
        body = [marker["rest"]] if marker["rest"] else []
        body.extend(line for line in lines[marker["idx"] + 1:end]
                    if not BOOK_RE.match(line))
        text = "\n".join(line for line in body if line.strip()).strip()
        if text:
            chapters.append(Chapter(book=marker["book"],
                                    number=marker["value"], text=text))
    return chapters


POLITICS_TOKEN_RE = re.compile(r"^[IVXLCivxlc1l|ΐίιΙІХхШшПп.]+$")


def _parse_politics(lines: list[str]) -> list[Chapter]:
    """«Политика»: римская глава, сразу за ней номер абзаца «1.».

    OCR читает «II 1.» то как «I I I .», то как «II 1.»; номер главы —
    римское число без последней единицы.
    """
    chapters: list[Chapter] = []
    book: str | None = None
    book_no = 0
    chapter_no = 0
    current: Chapter | None = None
    body: list[str] = []

    def flush():
        nonlocal current, body
        if current is not None:
            current.text = "\n".join(body).strip()
            if current.text:
                chapters.append(current)
            body = []

    for line in lines:
        m_book = BOOK_RE.match(line)
        if m_book:
            flush()
            current = None
            book_no += 1
            chapter_no = 0
            book = ROMAN[book_no - 1] if book_no <= len(ROMAN) else str(book_no)
            continue

        tokens = line.split()
        started = False
        # Перед римской цифрой иногда стоит номер страницы («зо I I .»,
        # «ίο Ι 1.»), поэтому пробуем начать разбор с первых трёх токенов.
        for start in range(min(3, len(tokens))):
            k = start
            while k < len(tokens) and POLITICS_TOKEN_RE.match(tokens[k]):
                k += 1
            if k == start:
                continue
            run = "".join(tokens[start:k]).rstrip(".")
            value = None
            if len(run) >= 2 and run[-1] in "1Il|ΐίιΙІi":
                value = roman_value(run[:-1])
            if value and value == chapter_no + 1:
                flush()
                current = Chapter(book=book, number=value, text="")
                chapter_no = value
                rest = " ".join(tokens[k:]).strip()
                if rest:
                    body.append(rest)
                started = True
                break
        if started:
            continue
        if current is not None:
            body.append(line)

    flush()
    return chapters


def parse_chapters(lines: list[str], style: str = "glava") -> list[Chapter]:
    """Делит поток строк на главы.

    style="glava" — книги и главы названы словами («КНИГА ПЕРВАЯ (А)»,
    «ГЛАВА ТРЕТЬЯ»), так набраны тома 1–3.
    style="numbered" — глава начинается номером с точкой и сразу текстом,
    так набран том 4 (например, «Большая этика»).
    style="ne" — «Никомахова этика»: глава отмечена римским числом в скобках.
    style="politics" — «Политика»: римская глава, за ней номер абзаца «1.».
    """
    if style == "ne":
        return _parse_ne(lines)
    if style == "politics":
        return _parse_politics(lines)

    chapters: list[Chapter] = []
    book: str | None = None
    book_no = 0
    chapter_no = 0
    current: Chapter | None = None
    body: list[str] = []

    def flush():
        nonlocal current, body
        if current is not None:
            current.text = "\n".join(body).strip()
            if current.text:
                chapters.append(current)
            body = []

    def begin(number: int, rest: str = ""):
        nonlocal current, chapter_no
        flush()
        current = Chapter(book=book, number=number, text="")
        chapter_no = number
        if rest:
            body.append(rest)

    for line in lines:
        m = BOOK_RE.match(line)
        if m:
            flush()
            current = None
            book_no += 1
            chapter_no = 0
            book = ROMAN[book_no - 1] if book_no <= len(ROMAN) else str(book_no)
            continue

        if style == "glava":
            m = CHAPTER_RE.match(line)
            if m:
                raw = m.group(1)
                if raw.isdigit():
                    # Цифра в заголовке — сам номер, ей и верим.
                    number = int(raw)
                else:
                    # Порядковое слово OCR рвёт («ПШСТАЯ» вместо «ШЕСТАЯ»), а
                    # порядок заголовков устойчив: номер берём по порядку.
                    number = chapter_no + 1
                    said = ordinal_to_number(raw)
                    if said is not None and said != number:
                        raise HeadingError(
                            f"заголовок стоит не на своём месте: {line} "
                            f"(ожидался номер {number})")
                begin(number)
                continue
        else:
            m = NUMBERED_RE.match(line)
            if m:
                begin(numbered(m.group(1)), m.group(2).strip())
                continue

        if current is not None:
            body.append(line)

    flush()
    return chapters


def numbered(raw: str) -> int:
    """Номер главы из «числа», прочитанного кириллицей или латиницей."""
    digits = "".join(DIGIT_LOOKALIKE.get(ch, ch) for ch in raw)
    return int(digits)


def chapter_path(slug: str, book: str | None, number: int) -> Path:
    name = f"{number:02d}.md"
    if book is None:
        return Path(slug) / name
    return Path(slug) / book / name


def cite(title: str, book: str | None, number: int) -> str:
    if book is None:
        return f"{title} {number}"
    return f"{title} {book.upper()}.{number}"
