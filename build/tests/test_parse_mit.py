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
    # Точное равенство, а не вхождение: "7" — законный короткий абзац
    # (число внутри текста, не маркер), _merge_stray_symbols не должен
    # его трогать и склеивать со следующим абзацем.
    assert chapters[0].text == (
        "first chapter text\n\nhe counted them:\n\n7\n\nstill the first chapter"
    )


def test_bare_number_ignored_when_work_uses_parts():
    raw = (
        "W\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nPart 1 \n\nтело\n\n2 \n\nэто не глава, а число в тексте\n\n"
        "Part 2 \n\nвторая\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert [c.number for c in chapters] == [1, 2]
    assert "не глава" in chapters[0].text


def test_glued_part_marker_returns_remainder_as_chapter_text():
    # Как в on-youth-and-old-age/posterior-analytics: строка-маркер
    # склеена без пустой строки с первым словом реального текста главы
    # ("Part 15 Some animals..."). Остаток должен войти в текст главы,
    # а не потеряться.
    raw = (
        "W\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nPart 1 \n\nfirst chapter\n\n"
        "Part 2 Some animals occupy the water, others live on land,\n"
        "and that being so, this chapter continues normally.\n\n"
        "Part 3 \n\nthird chapter\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert [c.number for c in chapters] == [1, 2, 3]
    assert chapters[1].text == (
        "Some animals occupy the water, others live on land, "
        "and that being so, this chapter continues normally."
    )


def test_glued_marker_symbol_does_not_become_its_own_paragraph():
    # Как в metaphysics: маркер склеен с одиночным символом-кавычкой
    # ("Part 2 \""), а настоящий абзац отделён от него пустой строкой.
    # Кавычка не должна стать отдельным "абзацем" из одного знака —
    # она приклеивается к началу первого непустого текста после неё.
    raw = (
        "W\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nPart 1 \n\nfirst chapter\n\n"
        "Part 2 \"\n\n"
        "\"Since we are seeking this knowledge, we must inquire of what kind\n"
        "are the causes and the principles.\n\n"
        "Part 3 \n\nthird chapter\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert chapters[1].number == 2
    paragraphs = chapters[1].text.split("\n\n")
    assert not any(len(p.strip()) <= 1 for p in paragraphs), \
        "ни один абзац не должен состоять из одного знака препинания"
    assert paragraphs[0].startswith('" "Since we are seeking this knowledge')


def test_standalone_stray_symbol_line_merges_into_next_paragraph():
    # Живая находка при перепроверке metaphysics: осиротевшая кавычка не
    # всегда приклеена к маркеру — иногда это отдельная строка "Part 1"
    # (чистая, без хвоста), пустая строка, отдельная строка из одной
    # кавычки, снова пустая строка, и только потом настоящий абзац. Это
    # уже не про склейку маркера, а про тело главы вообще — чинится общим
    # правилом в _reflow, а не только для строки-открывашки.
    raw = (
        "W\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nPart 1 \n\n\"\n\n"
        "\"The investigation of the truth is hard in one way, easy in another.\n"
        "An indication of this is found in the fact that no one attains it.\n\n"
        "Part 2 \n\nsecond chapter\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert chapters[0].number == 1
    paragraphs = chapters[0].text.split("\n\n")
    assert not any(len(p.strip()) <= 1 for p in paragraphs), \
        "ни один абзац не должен состоять из одного знака препинания"
    assert paragraphs[0].startswith(
        '" "The investigation of the truth is hard in one way'
    )


def test_reflow_leaves_legitimate_short_paragraphs_untouched():
    # Отрицательный тест на воздержание: абзац из одной буквы или цифры —
    # законный текст (перечисление, ссылка на пункт, число), а не осколок
    # склеенного маркера. _merge_stray_symbols должен пропустить такую
    # главу через _reflow вообще без изменений — точное равенство, а не
    # вхождение.
    raw = (
        "W\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nPart 1 \n\nfirst paragraph\n\nA\n\n7\n\nlast paragraph\n\n"
        "Part 2 \n\nsecond chapter\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert chapters[0].text == "first paragraph\n\nA\n\n7\n\nlast paragraph"


def test_stray_part_looking_line_with_matching_number_is_contained():
    # Расширение PART_RE — риск: обычная фраза, случайно начавшая строку
    # словами "Part 2 ...", неотличима от настоящего маркера, если число
    # совпадает с ожидаемым следующим номером (last + 1) — глава откроется
    # ошибочно. Но урон локален: следующий настоящий маркер (Part 3)
    # по-прежнему требует строгого +1 и синхронизируется корректно, а не
    # теряется вместе со всем, что после него.
    raw = (
        "W\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nPart 1 \n\nfirst chapter begins here.\n\n"
        "Part 2 of the discussion is developed further below, by coincidence.\n\n"
        "Part 3 \n\nreal third chapter text\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert [c.number for c in chapters] == [1, 2, 3]
    assert chapters[1].text == (
        "of the discussion is developed further below, by coincidence."
    )
    assert chapters[2].text == "real third chapter text"


def test_single_stray_part_line_does_not_flip_bare_number_style():
    # Раньше единственной строки, похожей на "Part N", было достаточно,
    # чтобы выключить голые цифры во всей работе — здесь это привело бы
    # к нулю глав (ParseError), хотя работа целиком в голом-цифровом
    # стиле. Теперь для part-стиля нужно, чтобы среди строк "Part N"
    # нашлись оба первых номера — 1 и 2, а не один случайный.
    raw = (
        "W\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\n1 \n\nfirst chapter text.\n\n"
        "Part 7 of an unrelated treatise argues differently, but that is "
        "beside the point.\n\n"
        "2 \n\nsecond chapter\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert [c.number for c in chapters] == [1, 2]
    assert [c.book for c in chapters] == [None, None]
    assert "second chapter" in chapters[1].text


def test_rejects_text_without_header_rule():
    with pytest.raises(ParseError, match="шапк"):
        parse("Nicomachean Ethics\nBy Aristotle\nno rule line here\n")


def test_rejects_text_without_chapters():
    raw = "W\nBy Aristotle\n\nTranslated by X\n\n" + "-" * 70 + "\n\nтекст\n"
    with pytest.raises(ParseError, match="глав"):
        parse(raw)
