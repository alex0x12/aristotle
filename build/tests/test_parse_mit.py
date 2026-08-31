from pathlib import Path
import pytest
from parse_mit import parse, Chapter, ParseError

FIXTURE = Path(__file__).parent / "fixtures" / "ne_head.mb.txt"
RAW = FIXTURE.read_text(encoding="utf-8")


def test_extracts_translator():
    translator, _ = parse(RAW)
    assert translator == "W. D. Ross"


def test_first_chapter_starts_correctly():
    _, chapters = parse(RAW)
    first = chapters[0]
    assert first.book == "I"
    assert first.number == 1
    assert first.text.startswith("Every art and every inquiry")


def test_drops_copyright_tail():
    _, chapters = parse(RAW)
    joined = "\n".join(c.text for c in chapters)
    assert "Copyright statement" not in joined
    assert "THE END" not in joined
    assert "Internet Classics Archive" not in joined


def test_reflows_hard_wrapped_lines():
    _, chapters = parse(RAW)
    body = chapters[0].text
    assert "\n" not in body.split("\n\n")[0], "абзац должен быть одной строкой"


def test_chapter_numbers_are_consecutive_within_book():
    _, chapters = parse(RAW)
    by_book: dict[str | None, list[int]] = {}
    for c in chapters:
        by_book.setdefault(c.book, []).append(c.number)
    for nums in by_book.values():
        assert nums == list(range(1, len(nums) + 1))


def test_stray_number_does_not_open_chapter():
    raw = (
        "Some Work\nBy Aristotle\n\nTranslated by Nobody\n\n"
        + "-" * 70
        + "\n\nBOOK I\n\n1 \n\nfirst chapter text\n\n"
        "he counted them: \n\n7 \n\nstill the first chapter\n\n"
        "2 \n\nsecond chapter text\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert [c.number for c in chapters] == [1, 2]
    assert "still the first chapter" in chapters[0].text


def test_work_without_books():
    raw = (
        "Poetics\nBy Aristotle\n\nTranslated by S. H. Butcher\n\n"
        + "-" * 70
        + "\n\n1 \n\nfirst section\n\n2 \n\nsecond section\n\nTHE END\n"
    )
    translator, chapters = parse(raw)
    assert translator == "S. H. Butcher"
    assert [c.book for c in chapters] == [None, None]
    assert [c.number for c in chapters] == [1, 2]


def test_rejects_text_without_header_rule():
    with pytest.raises(ParseError, match="шапк"):
        parse("Nicomachean Ethics\nBy Aristotle\nno rule line here\n")
