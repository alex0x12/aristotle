from pathlib import Path
import pytest
from parse_mit import Chapter, ParseError, parse, roman_to_int

FIX = Path(__file__).parent / "fixtures"
BOOKS = (FIX / "mit_books.mb.txt").read_text(encoding="utf-8")
SECTIONS = (FIX / "mit_sections.mb.txt").read_text(encoding="utf-8")


def test_roman_to_int():
    assert roman_to_int("I") == 1
    assert roman_to_int("IV") == 4
    assert roman_to_int("XII") == 12
    assert roman_to_int("XXVI") == 26


def test_extracts_translator():
    translator, _ = parse(BOOKS)
    assert translator == "W. D. Ross"


def test_bare_number_style_first_chapter():
    _, chapters = parse(BOOKS)
    first = chapters[0]
    assert first.book == "I"
    assert first.number == 1
    assert first.text.startswith("Every art and every inquiry")


def test_book_resets_chapter_numbering():
    _, chapters = parse(BOOKS)
    by_book: dict[str | None, list[int]] = {}
    for c in chapters:
        by_book.setdefault(c.book, []).append(c.number)
    assert "II" in by_book, "фикстура должна захватывать вторую книгу"
    for nums in by_book.values():
        assert nums == list(range(1, len(nums) + 1))


def test_drops_copyright_tail():
    _, chapters = parse(BOOKS)
    joined = "\n".join(c.text for c in chapters)
    assert "Copyright statement" not in joined
    assert "THE END" not in joined
    assert "Internet Classics Archive" not in joined


def test_reflows_hard_wrapped_lines():
    _, chapters = parse(BOOKS)
    assert "\n" not in chapters[0].text.split("\n\n")[0]


def test_part_style_with_roman_numbers():
    translator, chapters = parse(SECTIONS)
    assert translator == "S. H. Butcher"
    assert [c.number for c in chapters[:3]] == [1, 2, 3]
    assert chapters[-1].number == 26


def test_sections_are_not_books_and_do_not_reset_numbering():
    _, chapters = parse(SECTIONS)
    assert all(c.book is None for c in chapters), "SECTION книгой не является"
    numbers = [c.number for c in chapters]
    assert numbers == list(range(1, len(numbers) + 1)), \
        "нумерация идёт сквозь секции непрерывно"


def test_stray_number_does_not_open_chapter():
    raw = (
        "Some Work\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nBOOK I\n\nPart 1 \n\nfirst chapter text\n\n"
        "he counted them: \n\n7 \n\nstill the first chapter\n\n"
        "Part 2 \n\nsecond chapter text\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert [c.number for c in chapters] == [1, 2]
    assert "still the first chapter" in chapters[0].text


def test_bare_number_ignored_when_work_uses_parts():
    raw = (
        "W\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nPart 1 \n\nтело\n\n2 \n\nэто не глава, а число в тексте\n\n"
        "Part 2 \n\nвторая\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert [c.number for c in chapters] == [1, 2]
    assert "не глава" in chapters[0].text


def test_rejects_text_without_header_rule():
    with pytest.raises(ParseError, match="шапк"):
        parse("Nicomachean Ethics\nBy Aristotle\nno rule line here\n")


def test_rejects_text_without_chapters():
    raw = "W\nBy Aristotle\n\nTranslated by X\n\n" + "-" * 70 + "\n\nтекст\n"
    with pytest.raises(ParseError, match="глав"):
        parse(raw)
