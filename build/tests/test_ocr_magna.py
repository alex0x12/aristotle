"""Нормализация OCR-маркеров глав в «Большой этике» (style="numbered")."""
import parse_pdf
from pdf_corpus import fix_numbered_ocr


def chapters(lines):
    return parse_pdf.parse_chapters(fix_numbered_ocr(lines), style="numbered")


def test_brace_instead_of_one():
    got = chapters(["{. Собираясь говорить о вопросах этики"])

    assert [(c.book, c.number) for c in got] == [(None, 1)]
    assert got[0].text == "Собираясь говорить о вопросах этики"


def test_one_instead_of_seven_by_order():
    lines = [f"{n}. Текст {n}" for n in range(1, 7)]
    lines.append("1. Желая определить, что такое добродетель")

    got = chapters(lines)

    assert [c.number for c in got] == [1, 2, 3, 4, 5, 6, 7]
    assert got[6].text == "Желая определить, что такое добродетель"


def test_brace_inside_seventeen():
    lines = [f"{n}. Текст {n}" for n in range(1, 17)]
    lines.append("1{7. А теперь надо рассмотреть, как следует обращаться")

    got = chapters(lines)

    assert [c.number for c in got] == list(range(1, 18))
    assert got[16].text == "А теперь надо рассмотреть, как следует обращаться"


def test_ordinary_numbers_untouched():
    lines = ["1. Первая", "2. Вторая", "3. Третья"]
    fixed = fix_numbered_ocr(lines)

    assert fixed == lines
    assert [c.number for c in chapters(lines)] == [1, 2, 3]


def test_book_resets_numbering():
    lines = [
        "КНИГА ПЕРВАЯ (А)",
        "1. Первая",
        "2. Вторая",
        "КНИГА ВТОРАЯ (В)",
        "{. После этого следует рассмотреть порядочность",
        "2. Сознательность",
    ]

    got = chapters(lines)

    assert [(c.book, c.number) for c in got] == [
        ("i", 1), ("i", 2), ("ii", 1), ("ii", 2)]
    assert got[2].text == "После этого следует рассмотреть порядочность"
