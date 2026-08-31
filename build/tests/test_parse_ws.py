import json
import pytest
from parse_mit import ParseError
from parse_ws import (book_subpages, chapter_level, check_usable,
                      clean_wikitext, page_title, parse_page)

APOS = "'" * 3

PART_PAGE = f"""{{{{header
 | translator = Arthur Platt
 | section    = {APOS}Book I{APOS}
}}}}

== Part 1 ==

WE have now discussed the other parts of animals.

There are four causes underlying everything.

== Part 2 ==

Now the female is as it were a deformed male.
"""

# «Политика (Эллис)»: книга — второй уровень, глава — третий.
NESTED_PAGE = """{{header
 | translator = William Ellis
 | section  = Book 1
}}

== BOOK I ==

=== CHAPTER I ===

Every state is a community of some kind.

=== CHAPTER II ===

He who thus considers things in their first growth.

=== CHAPTER III ===

Seeing then that the state is made up of households.
"""

BARE_HEADINGS = """{{header
 | translator = A. S. L. Farquharson
}}

== 1 ==

первая глава

== 2 ==

вторая глава
"""

NO_HEADINGS = """{{header
 | translator = Joseph Solomon
}}

Весь текст одним куском, заголовков нет.

Второй абзац.
"""

# «О возникновении животных», книги II-V: заголовков == нет вовсе, границы
# глав — голые числа на отдельной строке (проверено на живых данных).
BARE_NUMBER_PAGE = """{{header
 | translator = Arthur Platt
 | section    = Book II
}}


Book II


1

THAT the male and the female are the principles of generation.

2

Second chapter of book two.

3

Third chapter of book two.

4

Fourth chapter of book two.

5

Fifth chapter of book two.

6

Sixth chapter of book two.

7

Seventh chapter of book two.

8

Eighth chapter of book two.
"""

# «О снах»: маркеры глав — строки "Part N" обычным текстом, без разметки ==
# == вовсе (проверено на живых данных, .cache/180ff90f0f7dc740.txt).
PLAIN_PART_PAGE = """{{header
 | translator = John Isaac Beare
}}


Part 1
{{dhr}}
We must, in the next place, investigate the subject of the dream.

Part 2
{{dhr}}
We can best obtain a scientific view of the nature of the dream.

Part 3
{{dhr}}
From this it is manifest that the stimulatory movements based upon sensory impressions.
"""

# Ничья: одна книга (уровень 2) и одна глава (уровень 3) на странице.
TIE_PAGE = """{{header
 | translator = William Ellis
}}

== BOOK VIII ==

=== CHAPTER I ===

Music seems to demand a similar mode of expression.
"""

INTRODUCTION_PAGE = """{{header
 | translator = X
}}

== Introduction ==

Вводный текст без номера.

== Conclusion ==

Заключительный текст без номера.
"""


def test_page_title_from_url():
    assert page_title("https://en.wikisource.org/wiki/Politics_(Ellis)") == "Politics (Ellis)"
    assert page_title("https://en.wikisource.org/wiki/On_Dreams_(Aristotle)") == "On Dreams (Aristotle)"


def test_chapter_level_picks_most_frequent():
    assert chapter_level(PART_PAGE) == 2
    assert chapter_level(NESTED_PAGE) == 3, "у Политики глав больше, чем книг"
    assert chapter_level(BARE_HEADINGS) == 2
    assert chapter_level(NO_HEADINGS) is None


def test_parse_page_part_style():
    translator, chapters = parse_page(PART_PAGE, "I")
    assert translator == "Arthur Platt"
    assert [c.number for c in chapters] == [1, 2]
    assert all(c.book == "I" for c in chapters)
    assert chapters[0].text.startswith("WE have now discussed")
    assert "\n\n" in chapters[0].text


def test_nested_headings_book_line_is_not_a_chapter():
    _, chapters = parse_page(NESTED_PAGE, "I")
    assert [c.number for c in chapters] == [1, 2, 3], \
        "строка BOOK I главой быть не должна"
    assert chapters[0].text == "Every state is a community of some kind."
    assert "BOOK" not in "\n".join(c.text for c in chapters)


def test_bare_numeric_headings_are_chapters():
    _, chapters = parse_page(BARE_HEADINGS, None)
    assert [c.number for c in chapters] == [1, 2]
    assert chapters[0].text == "первая глава"


def test_unnumbered_same_level_heading_ends_chapters():
    # На живых данных (On Longevity and Shortness of Life) страница после
    # последней настоящей главы несёт "== External links==" — заголовок
    # того же уровня, что и главы, но без номера. Без этой проверки он
    # становится фиктивной главой, а сырая внешняя ссылка в его теле
    # утекает в корпус (ловится проверкой шага 8 на "Internet Classics
    # Archive").
    raw = ("{{header | translator = X }}\n\n"
           "== 1 ==\n\nглава одна\n\n"
           "== External links==\n\n*[http://classics.mit.edu|Archive]\n")
    _, chapters = parse_page(raw, None)
    assert [c.number for c in chapters] == [1]
    assert "External" not in chapters[0].text
    assert "classics.mit.edu" not in chapters[0].text


def test_page_without_headings_is_one_chapter():
    translator, chapters = parse_page(NO_HEADINGS, None)
    assert translator == "Joseph Solomon"
    assert len(chapters) == 1
    assert chapters[0].number == 1
    assert "Второй абзац." in chapters[0].text


def test_bare_numbers_are_chapters_when_no_headings_at_all():
    # Находка ревью: «О возникновении животных», книги II-V не размечены
    # заголовками == вовсе — только голые числа на отдельной строке.
    # Без распознавания голых чисел вся книга схлопывалась в одну
    # 90-килобайтную "главу".
    _, chapters = parse_page(BARE_NUMBER_PAGE, "II")
    assert [c.number for c in chapters] == [1, 2, 3, 4, 5, 6, 7, 8]
    assert all(c.book == "II" for c in chapters)
    assert chapters[0].text.startswith("THAT the male and the female")
    assert chapters[-1].text == "Eighth chapter of book two."


def test_plain_text_part_markers_are_chapters():
    # Находка ревью: "О снах" не размечен ни заголовками ==, ни голыми
    # числами — только строками "Part N" обычным текстом. Без этого пути
    # разбор сваливался в запасной вариант "вся страница — одна глава", и
    # строки-маркеры оставались в теле текста.
    translator, chapters = parse_page(PLAIN_PART_PAGE, None)
    assert translator == "John Isaac Beare"
    assert [c.number for c in chapters] == [1, 2, 3]
    assert chapters[0].text.startswith("We must, in the next place")
    assert chapters[1].text.startswith("We can best obtain")
    assert chapters[2].text.startswith("From this it is manifest")
    joined = "\n".join(c.text for c in chapters)
    assert "Part 1" not in joined
    assert "Part 2" not in joined
    assert "Part 3" not in joined


def test_plain_text_part_markers_require_sequential_numbers():
    # Одна случайная строка прозы вида "Part 5 ..." без предшествующих
    # "Part 1".."Part 4" не должна открывать главу.
    raw = ("{{header | translator = X }}\n\n"
           "Some introductory text mentioning Part 5 of a different work.\n\n"
           "More text.\n")
    _, chapters = parse_page(raw, None)
    assert len(chapters) == 1
    assert "Part 5" in chapters[0].text


def test_chapter_level_tie_prefers_deeper_level():
    # Находка ревью: при равном числе заголовков разных уровней должен
    # побеждать более глубокий — главы всегда глубже книг.
    assert chapter_level(TIE_PAGE) == 3


def test_tie_page_chapter_gets_correct_number_not_book_number():
    _, chapters = parse_page(TIE_PAGE, "VIII")
    assert [c.number for c in chapters] == [1], \
        "глава должна получить номер CHAPTER I (1), а не BOOK VIII (8)"
    assert chapters[0].book == "VIII"
    assert "Music seems" in chapters[0].text


def test_unnumbered_non_apparatus_heading_uses_sequential_fallback():
    # Находка ревью: обрыв по любому безномерному заголовку слишком широк —
    # работы с заголовками-словами (Introduction, Conclusion...) не аппарат,
    # а обычные главы, и должны нумероваться по порядку, не обрывать разбор.
    _, chapters = parse_page(INTRODUCTION_PAGE, None)
    assert [c.number for c in chapters] == [1, 2]
    assert chapters[0].text == "Вводный текст без номера."
    assert chapters[1].text == "Заключительный текст без номера."
    assert "==" not in "\n".join(c.text for c in chapters)


def test_clean_strips_nested_templates():
    src = "{{translation license|original={{PD-old}}|translation={{PD-1923}}}} текст"
    assert clean_wikitext(src).strip() == "текст"


def test_clean_strips_refs_and_categories():
    src = ("текст<ref>сноска с [[ссылкой]]</ref> дальше\n"
           "[[Category:Aristotle]]\n[[fr:Politique]]\n")
    out = clean_wikitext(src)
    assert "сноска" not in out
    assert "Category" not in out
    assert "fr:" not in out
    assert "текст дальше" in out


def test_clean_keeps_link_text():
    assert clean_wikitext("см. [[Physics|Физику]] и [[Topics]]") == "см. Физику и Topics"


def test_check_usable_rejects_transclusion():
    raw = '{{header}}\n<pages index="Works of Aristotle v9.djvu" from=425 to=437 />'
    with pytest.raises(ParseError, match="трансклюзи"):
        check_usable(raw, "Eudemian Ethics")


def test_check_usable_rejects_redirect():
    with pytest.raises(ParseError, match="редирект"):
        check_usable("#redirect [[Bring me the sunset in a cup]]", "Problems")


def test_check_usable_accepts_normal_page():
    check_usable(PART_PAGE, "On the Generation of Animals/Book I")


def test_book_subpages_filters_non_books_and_sorts():
    def fake_get(url):
        return json.dumps({"query": {"allpages": [
            {"title": "Politics (Ellis)/Book 10"},
            {"title": "Politics (Ellis)/Book 2"},
            {"title": "Politics (Ellis)/Introduction"},
            {"title": "Politics (Ellis)/Book 1"},
        ]}})

    pages = book_subpages("Politics (Ellis)", fake_get)
    assert [name for name, _ in pages] == [
        "Politics (Ellis)/Book 1",
        "Politics (Ellis)/Book 2",
        "Politics (Ellis)/Book 10",
    ]
    assert [book for _, book in pages] == ["I", "II", "X"]


def test_book_subpages_accepts_roman():
    def fake_get(url):
        return json.dumps({"query": {"allpages": [
            {"title": "On the Generation of Animals/Book II"},
            {"title": "On the Generation of Animals/Book I"},
        ]}})

    assert [b for _, b in book_subpages("On the Generation of Animals", fake_get)] == ["I", "II"]


def test_book_subpages_empty_when_no_subpages():
    def fake_get(url):
        return json.dumps({"query": {"allpages": []}})

    assert book_subpages("On Dreams (Aristotle)", fake_get) == []
