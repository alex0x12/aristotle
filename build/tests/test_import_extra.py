from pathlib import Path

import import_extra
import parse_pdf


def test_athenian_sections_become_chapters():
    lines = [
        "ЧАСТЬ ПЕРВАЯ",
        "I. Килонова смута",          # оглавление — до тела
        "ЧАСТЬ ПЕРВАЯ*",
        "1… [Судили их триста судей] из благородных родов.",
        "продолжение.",
        "2. После этого происходили раздоры.",
        "3. Порядок древнего устройства.",
    ]
    chapters = import_extra.parse_athenian(lines)
    assert [c.number for c in chapters] == [1, 2, 3]
    assert chapters[0].book is None
    assert chapters[0].text.startswith("[Судили их")


def test_history_books_and_chapters():
    lines = [
        "Предисловие, где Книга третья упоминается в тексте.",
        "Книга первая",
        "Глава I",
        "Первый текст.",
        "Глава II",
        "Второй текст.",
        "Книга вторая",
        "ГЛАВА 1",
        "Третий текст.",
        "ГЛАВА Χ",                     # греческая хи = X
        "Четвёртый текст.",
        "Книга десятая",
        "ГЛАВА I",
        "Спорная книга.",
        "ПРИМЕЧАНИЯ",
        "ГЛАВА II",
    ]
    chapters = import_extra.parse_history(lines)
    assert [(c.book, c.number) for c in chapters] == [
        ("i", 1), ("i", 2), ("ii", 1), ("ii", 10)
    ]
    assert chapters[0].text == "Первый текст."


def test_memory_sections():
    lines = [
        "вступление",
        "                        I",
        "Речь пойдет о том, что есть, по какой причине.",
        "",
        "                        II",
        "Второй раздел.",
        "КОММЕНТАРИЙ",
        "не текст",
    ]
    chapters = import_extra.parse_memory(lines)
    assert [(c.book, c.number) for c in chapters] == [(None, 1), (None, 2)]
    assert chapters[0].text.startswith("Речь пойдет")
    assert "не текст" not in chapters[1].text


def test_dreams_three_chapters():
    lines = [
        "abstract на английском",
        "   1. Теперь необходимо изучить вопрос о сновидении.",
        "текст первой.",
        "   2. Что же такое сновидение?",
        "текст второй.",
        "   3. Из сказанного ясно.",
        "текст третьей.",
        "                                Литература",
        "сноска",
    ]
    chapters = import_extra.parse_dreams(lines)
    assert [c.number for c in chapters] == [1, 2, 3]
    assert chapters[2].text.startswith("3. Из сказанного")


def test_sense_splits_seven_roman_sections():
    filler = [f"строка {i}" for i in range(205)]
    lines = (
        ["После того как о душе самой по себе уже подробно изложено."]
        + filler
        + ["                        I", "Первый раздел."]
        + ["                        II", "Второй."]
        + ["                        III", "Третий."]
        + ["                        IV", "Четвёртый."]
        + ["                        V", "Пятый."]
        + ["                        VI", "Шестой."]
        + ["                        VII", "Седьмой."]
        + ["                        I", "Речь пойдет о памяти."]
    )
    chapters = import_extra.parse_sense(lines)
    assert [c.number for c in chapters] == [1, 2, 3, 4, 5, 6, 7]
    assert chapters[0].text.startswith("После того как")


def test_book_only_source_goes_one_file_per_book():
    lines = [
        "Предисловие.",
        "КНИГА ПЕРВАЯ (А) 1 Первый текст.",
        "2 О ЧАСТЯХ ЖИВОТНЫХ продолжение первого.",
        "КНИГА ПЕРВАЯ (А) 3 ещё первый.",
        "КНИГА ВТОРАЯ (В) 1 Второй текст.",
        "ПРИМЕЧАНИЯ",
        "не текст",
    ]
    chapters = import_extra.parse_parts_books(lines)
    assert [(c.book, c.number) for c in chapters] == [("i", 1), ("ii", 1)]
    assert "Первый текст" in chapters[0].text
    assert "ещё первый" in chapters[0].text
    assert "КНИГА" not in chapters[0].text
    assert "О ЧАСТЯХ ЖИВОТНЫХ" not in chapters[0].text
    assert chapters[1].text.startswith("Второй текст")


def test_frontmatter_marks_source_as_file():
    chapter = parse_pdf.Chapter(book="ii", number=3, text="текст")
    text = import_extra.frontmatter("history-of-animals", chapter)
    assert 'work: "История животных"' in text
    assert "book: \"ii\"" in text
    assert "source: file" in text
    assert 'cite: "История животных II.3"' in text


def test_write_work_uses_corpus_paths(tmp_path):
    chapters = [parse_pdf.Chapter(book="i", number=1, text="текст")]
    import_extra.write_work("history-of-animals", chapters, tmp_path)
    path = tmp_path / "history-of-animals" / "i" / "01.md"
    assert path.exists()
    assert path.read_text(encoding="utf-8").startswith("---\nwork:")
