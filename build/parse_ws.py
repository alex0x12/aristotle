import json
import re
import time
from urllib.parse import quote, unquote

from parse_mit import Chapter, ParseError, roman_to_int

API = "https://en.wikisource.org/w/api.php"
RAW = "https://en.wikisource.org/w/index.php"
DELAY = 1.0  # API отвечает 429 на частые запросы

TRANSLATOR_RE = re.compile(r"^\s*\|\s*translator\s*=\s*(.+?)\s*$", re.M)
HEADING_RE = re.compile(r"^(=+)\s*(.+?)\s*=+\s*$")
NUMBER_RE = re.compile(r"(\d+|[IVXLC]+)\s*$")
BARE_NUMBER_RE = re.compile(r"^\s*(\d+)\s*$")
# «О снах»: строки-маркеры вида "Part 1", "Part 2" стоят обычным текстом,
# без разметки == == — Wikisource иногда воспроизводит то же соглашение об
# именовании частей, что и Internet Classics Archive (см. PART_RE в
# parse_mit.py), но без оборачивающих знаков заголовка.
PART_LINE_RE = re.compile(r"^\s*Part\s+(\d+|[IVXLC]+)\s*$")
BOOK_SUB_RE = re.compile(r"/Book\s+(\d+|[IVXLC]+)\s*$", re.I)

# Служебные разделы аппарата, на которых разбор обрывается: заголовок этого
# уровня без номера здесь означает конец текста, а не главу без номера.
# Сравнение без учёта регистра.
APPARATUS_HEADINGS = {
    "external links", "references", "notes", "see also",
    "bibliography", "footnotes", "source", "sources",
}
REF_RE = re.compile(r"<ref[^>]*>.*?</ref>|<ref[^>]*/>", re.S | re.I)
TAG_RE = re.compile(r"</?[a-z][^>]*>", re.I)
DROP_LINK_RE = re.compile(r"\[\[(?:Category|File|Image|[a-z]{2,3}):[^\]]*\]\]", re.I)
LINK_RE = re.compile(r"\[\[(?:[^\]|]*\|)?([^\]]*)\]\]")
BOLD_RE = re.compile(r"'{2,}")
TRANSCLUSION_RE = re.compile(r"<pages\b", re.I)
REDIRECT_RE = re.compile(r"^\s*#redirect", re.I)

ROMAN_OUT = ["", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X",
             "XI", "XII", "XIII", "XIV", "XV"]


def page_title(url: str) -> str:
    return unquote(url.rsplit("/", 1)[-1]).replace("_", " ")


def _to_int(token: str) -> int:
    return int(token) if token.isdigit() else roman_to_int(token)


def check_usable(raw: str, page: str) -> None:
    """Отвергает страницы, из которых текст не достать.

    Обе формы отдают правдоподобный ответ с кодом 200, и без этой проверки
    в корпус попадут главы, состоящие из шаблона вместо слов Аристотеля.
    """
    if REDIRECT_RE.match(raw):
        raise ParseError(f"{page}: страница — редирект, а не текст")
    if TRANSCLUSION_RE.search(raw):
        raise ParseError(
            f"{page}: текст лежит трансклюзией Proofread-Page, "
            f"в сыром вики-тексте его нет")


def _strip_templates(text: str) -> str:
    """Снимает шаблоны, в том числе вложенные, изнутри наружу."""
    pattern = re.compile(r"\{\{[^{}]*\}\}")
    while True:
        text, count = pattern.subn("", text)
        if not count:
            return text


def clean_wikitext(text: str) -> str:
    text = REF_RE.sub("", text)
    text = _strip_templates(text)
    text = DROP_LINK_RE.sub("", text)
    text = LINK_RE.sub(r"\1", text)
    text = TAG_RE.sub("", text)
    return BOLD_RE.sub("", text)


def chapter_level(raw: str) -> int | None:
    """Уровень заголовка, которым размечены главы.

    У разных работ он разный: у «Политики» глава — третий уровень, а второй
    занят книгой. Берём тот уровень, которого на странице больше всего; при
    равенстве — более глубокий, потому что главы всегда глубже книг (книга
    из одной главы иначе отдала бы уровень книги, а не главы).
    """
    counts: dict[int, int] = {}
    for line in raw.splitlines():
        match = HEADING_RE.match(line)
        if match:
            level = len(match.group(1))
            counts[level] = counts.get(level, 0) + 1
    if not counts:
        return None
    return max(counts, key=lambda level: (counts[level], level))


def book_subpages(title: str, get) -> list[tuple[str, str]]:
    url = (f"{API}?action=query&list=allpages"
           f"&apprefix={quote(title + '/')}&aplimit=200&format=json")
    data = json.loads(get(url))
    found: list[tuple[int, str]] = []
    for page in data.get("query", {}).get("allpages", []):
        match = BOOK_SUB_RE.search(page["title"])
        if match:
            found.append((_to_int(match.group(1)), page["title"]))
    found.sort()
    return [(name, ROMAN_OUT[number]) for number, name in found]


def _reflow(lines: list[str]) -> str:
    body = clean_wikitext("\n".join(lines)).strip()
    return re.sub(r"\n{3,}", "\n\n", body)


def _heading_chapters(lines: list[str], level: int,
                       book: str | None) -> list[Chapter]:
    chapters: list[Chapter] = []
    number: int | None = None
    buf: list[str] = []

    def flush() -> None:
        if number is not None:
            chapters.append(Chapter(book=book, number=number, text=_reflow(buf)))

    for line in lines:
        heading = HEADING_RE.match(line)
        if heading:
            if len(heading.group(1)) != level:
                continue  # заголовок другого уровня — не граница главы
            found = NUMBER_RE.search(heading.group(2))
            if found is None:
                title = heading.group(2).strip().lower()
                if title in APPARATUS_HEADINGS:
                    # Служебный раздел в конце страницы (External links,
                    # Notes и т.п.) — дальше текста уже нет, только аппарат;
                    # заканчиваем разбор, как parse_mit обрывает тело на
                    # строке "THE END".
                    flush()
                    number = None
                    break
                # Заголовок нужного уровня без номера, но не аппарат —
                # обычная глава без номера (Introduction, Conclusion...),
                # нумеруем по порядку.
                flush()
                number = len(chapters) + 1
                buf = []
                continue
            flush()
            number = _to_int(found.group(1))
            buf = []
            continue
        if number is not None:
            buf.append(line)
    flush()
    return chapters


def _bare_number_chapters(lines: list[str],
                           book: str | None) -> list[Chapter]:
    """Главы по голым числам на отдельной строке — для страниц без единого
    заголовка ==, как книги II-V "О возникновении животных". Тот же приём,
    что и в parse_mit: номер принимается только если он на единицу больше
    предыдущего, иначе случайное число в тексте открыло бы главу."""
    chapters: list[Chapter] = []
    number: int | None = None
    buf: list[str] = []
    last = 0

    def flush() -> None:
        if number is not None:
            chapters.append(Chapter(book=book, number=number, text=_reflow(buf)))

    for line in lines:
        match = BARE_NUMBER_RE.match(line)
        if match and int(match.group(1)) == last + 1:
            flush()
            number = last = int(match.group(1))
            buf = []
            continue
        if number is not None:
            buf.append(line)
    flush()
    return chapters


def _part_line_chapters(lines: list[str], book: str | None) -> list[Chapter]:
    """Главы по строкам-маркерам "Part N" обычным текстом, без разметки
    заголовка — как в "On Dreams" на Wikisource. Тот же приём
    последовательности, что и в _bare_number_chapters: номер принимается,
    только если он на единицу больше предыдущего, иначе случайное
    предложение, начинающееся со слова "Part", открыло бы главу."""
    chapters: list[Chapter] = []
    number: int | None = None
    buf: list[str] = []
    last = 0

    def flush() -> None:
        if number is not None:
            chapters.append(Chapter(book=book, number=number, text=_reflow(buf)))

    for line in lines:
        match = PART_LINE_RE.match(line)
        if match and _to_int(match.group(1)) == last + 1:
            flush()
            number = last = _to_int(match.group(1))
            buf = []
            continue
        if number is not None:
            buf.append(line)
    flush()
    return chapters


def parse_page(raw: str, book: str | None) -> tuple[str | None, list[Chapter]]:
    match = TRANSLATOR_RE.search(raw)
    translator = match.group(1) if match else None

    level = chapter_level(raw)
    lines = raw.splitlines()

    # Порядок распознавания границ главы: сначала заголовки нужного уровня;
    # если заголовков нет вовсе — голые числа на отдельной строке; если и
    # их нет — строки-маркеры "Part N" обычным текстом; и только если нет
    # ничего из этого — вся страница одной главой.
    if level is not None:
        chapters = _heading_chapters(lines, level, book)
    else:
        chapters = _bare_number_chapters(lines, book)
        if not chapters:
            chapters = _part_line_chapters(lines, book)

    if not chapters:
        body = _reflow(lines)
        if not body:
            raise ParseError("страница пуста: ни заголовков, ни текста")
        chapters = [Chapter(book=book, number=1, text=body)]

    return translator, chapters


def parse_work(work, get) -> tuple[str | None, list[Chapter]]:
    """Совместим по смыслу с parse_mit.parse, но ходит в сеть через get."""
    title = page_title(work.url)
    subpages = book_subpages(title, get)
    targets = subpages or [(title, None)]

    translator: str | None = None
    chapters: list[Chapter] = []
    for index, (name, book) in enumerate(targets):
        if index:
            time.sleep(DELAY)
        raw = get(f"{RAW}?title={quote(name)}&action=raw")
        check_usable(raw, name)
        found, page_chapters = parse_page(raw, book)
        translator = translator or found
        chapters.extend(page_chapters)

    if not chapters:
        raise ParseError(f"{work.slug}: не найдено ни одной главы")
    return translator, chapters
