from pathlib import Path

import pytest

import parse_pdf


def word(x0, y0, text, x1=None, height=10.0):
    return parse_pdf.Word(x0=x0, y0=y0, x1=x0 + (x1 or 20.0), y1=y0 + height, text=text)


# --- извлечение строк из слов страницы -------------------------------------
# Беккеровские номера стоят в поле: на чётных страницах слева (x < 45), на
# нечётных справа (x > ширина - 40). Тело набрано между полями.

def test_page_lines_drops_margin_numbers_on_both_sides():
    # Геометрия как в скане: номер стоит на своей базовой линии, смещённой
    # относительно строки набора, поэтому оказывается один в строке.
    words = [
        word(32.4, 75.9, "35"),        # поле слева
        word(51.8, 71.7, "сущность"),
        word(120.0, 71.7, "не"),
        word(315.3, 49.8, "25"),       # поле справа
        word(51.7, 86.0, "допускает"),
    ]
    assert parse_pdf.page_lines(words, width=349.44) == [
        "сущность не",
        "допускает",
    ]


def test_page_lines_keeps_body_that_starts_left_of_45():
    # На части страниц набор начинается с x≈33, а беккеровские номера стоят
    # справа. Резать тело по фиксированному левому полю нельзя.
    words = [
        word(33.1, 20.0, "тельство"),
        word(60.0, 20.0, "тому"),
        word(300.9, 8.0, "980а", x1=17.0),
    ]
    assert parse_pdf.page_lines(words, width=348.48) == ["тельство тому"]


def test_page_lines_keeps_lone_word_inside_the_column():
    # Одиночное слово в теле (короткий абзац) — не номер, его терять нельзя.
    words = [word(160.0, 20.0, "Аристотель")]
    assert parse_pdf.page_lines(words, width=348.48) == ["Аристотель"]


def test_page_lines_orders_words_by_geometry_within_a_line():
    words = [
        word(150.0, 20.0, "мыслит"),
        word(60.0, 20.0, "Ум"),
        word(60.0, 32.0, "второй"),
    ]
    assert parse_pdf.page_lines(words, width=349.44) == ["Ум мыслит", "второй"]


# --- склейка переносов -----------------------------------------------------

def test_join_hyphenated_glues_word_broken_at_line_end():
    lines = ["Все люди от природы стремятся к знанию. Доказа-", "тельство тому — влечение."]
    assert parse_pdf.join_hyphenated(lines) == [
        "Все люди от природы стремятся к знанию. Доказательство тому — влечение."
    ]


def test_join_hyphenated_keeps_dash_between_words():
    # Тире в конце строки — не перенос: следующее слово начинается с большой буквы.
    lines = ["... и причина этого в том, —", "Зрение больше всех чувств."]
    assert parse_pdf.join_hyphenated(lines) == [
        "... и причина этого в том, —",
        "Зрение больше всех чувств.",
    ]


# --- чистка остатков OCR ---------------------------------------------------

def test_strip_bekker_removes_margin_numbers_left_in_flow():
    assert parse_pdf.strip_bekker("познанию и обнаруживает много различий. »8ot>") == \
        "познанию и обнаруживает много различий."


def test_strip_bekker_removes_bekker_header_before_chapter_number():
    # Над главами «Большой этики» стоит колонтитул беккера («1181 п24»),
    # и он прилипает к началу главы, ломая распознавание номера.
    assert parse_pdf.strip_bekker("1181 п24 1. Собираясь говорить") == \
        "1. Собираясь говорить"


def test_strip_bekker_keeps_words():
    line = "И причина этого в том, что зрение больше всех других чувств"
    assert parse_pdf.strip_bekker(line) == line


def test_clean_lines_strips_numbers_before_joining_hyphens():
    # Номер беккера цепляется к концу строки («Доказа- 980а»), и без снятия
    # номера перенос не виден: строка кончается не дефисом, а цифрами.
    lines = ["знанию. Доказа- 980а", "тельство тому — влечение."]
    assert parse_pdf.clean_lines(lines) == [
        "знанию. Доказательство тому — влечение."
    ]


# --- разбор на книги и главы ----------------------------------------------

def test_ordinal_to_number_covers_first_fourteen():
    assert parse_pdf.ordinal_to_number("ПЕРВАЯ") == 1
    assert parse_pdf.ordinal_to_number("ДЕСЯТАЯ") == 10
    assert parse_pdf.ordinal_to_number("ЧЕТЫРНАДЦАТАЯ") == 14
    assert parse_pdf.ordinal_to_number("ВОСЕМНАДЦАТАЯ") == 18
    assert parse_pdf.ordinal_to_number("БЕЗЫМЯННАЯ") is None


def test_parse_chapters_splits_by_book_and_chapter():
    lines = [
        "МЕТАФИЗИКА",
        "КНИГА ПЕРВАЯ (А)",
        "ГЛАВА ПЕРВАЯ",
        "Все люди от природы стремятся к знанию.",
        "ГЛАВА ВТОРАЯ",
        "Мудрый и мудрость.",
        "КНИГА ВТОРАЯ (α)",
        "ГЛАВА ПЕРВАЯ",
        "Итак, прежде всего надо сказать.",
    ]
    chapters = parse_pdf.parse_chapters(lines)
    assert [(c.book, c.number) for c in chapters] == [
        ("i", 1), ("i", 2), ("ii", 1)
    ]
    assert chapters[0].text == "Все люди от природы стремятся к знанию."
    assert chapters[2].text == "Итак, прежде всего надо сказать."


def test_ordinal_to_number_handles_compound_phrases():
    # В издании главы бывают составными: «ДВАДЦАТЬ ПЕРВАЯ», «ТРИДЦАТЬ ВТОРАЯ».
    assert parse_pdf.ordinal_to_number("ДВАДЦАТЬ ПЕРВАЯ") == 21
    assert parse_pdf.ordinal_to_number("ТРИДЦАТЬ ВТОРАЯ") == 32
    assert parse_pdf.ordinal_to_number("СОРОК ШЕСТАЯ") == 46
    assert parse_pdf.ordinal_to_number("ДВАДЦАТЬ") is None
    # Круглые десятки тоже встречаются отдельным словом: «ГЛАВА ТРИДЦАТАЯ».
    assert parse_pdf.ordinal_to_number("ДВАДЦАТАЯ") == 20
    assert parse_pdf.ordinal_to_number("ТРИДЦАТАЯ") == 30


def test_parse_chapters_accepts_compound_headings():
    # Доходим до двадцать первой главы: до неё заголовки идут подряд.
    lines = ["КНИГА ПЕРВАЯ"]
    for i in range(1, 21):
        lines += [f"ГЛАВА {i}", "текст"]
    lines += ["ГЛАВА ДВАДЦАТЬ ПЕРВАЯ", "текст", "ГЛАВА ДВАДЦАТЬ ВТОРАЯ", "ещё"]
    chapters = parse_pdf.parse_chapters(lines)
    assert [c.number for c in chapters] == list(range(1, 23))


def test_parse_chapters_accepts_digit_chapter_headings():
    # В «Аналитиках» глав десятки, и в издании они набраны цифрами.
    lines = ["КНИГА ПЕРВАЯ", "ГЛАВА 46", "Итак, доказано.", "ГЛАВА 47", "Далее."]
    chapters = parse_pdf.parse_chapters(lines)
    assert [c.number for c in chapters] == [46, 47]
    assert chapters[0].text == "Итак, доказано."


def test_parse_chapters_accepts_ocr_typo_in_glava():
    # В «Физике» (том 3) первая глава набрана/распознана как «ГЛЛВА ПЕРВАЯ».
    lines = ["КНИГА ПЕРВАЯ (А)", "ГЛЛВА ПЕРВАЯ", "текст первой.",
             "ГЛАВА ВТОРАЯ", "текст второй."]
    chapters = parse_pdf.parse_chapters(lines)
    assert [c.number for c in chapters] == [1, 2]
    assert chapters[0].text == "текст первой."


def test_parse_chapters_accepts_ocr_typos_in_kniga():
    # В томе 3 встречаются «КИНГА» и «КПИГА» вместо «КНИГА»: без этого
    # сброс нумерации книги теряется и все главы после неё съезжают.
    lines = ["КПИГА ВОСЬМАЯ (Θ)", "ГЛАВА ПЕРВАЯ", "a",
             "ГЛАВА ВТОРАЯ", "b", "КИНГА ДЕВЯТАЯ", "ГЛАВА ПЕРВАЯ", "c"]
    chapters = parse_pdf.parse_chapters(lines)
    assert [(c.book, c.number) for c in chapters] == [
        ("i", 1), ("i", 2), ("ii", 1)
    ]


def test_parse_chapters_accepts_trailing_quote_after_heading():
    # На одной странице «Физики» заголовок распознан как «ГЛАВА ПЕРВАЯ «».
    lines = ["КНИГА ПЕРВАЯ (А)", "ГЛАВА ПЕРВАЯ «", "текст.",
             "ГЛАВА ВТОРАЯ", "ещё."]
    chapters = parse_pdf.parse_chapters(lines)
    assert [c.number for c in chapters] == [1, 2]


def test_parse_chapters_numbers_by_order_when_ordinal_is_garbled():
    # OCR порвал «ШЕСТАЯ» в «ПШСТАЯ»: номер берём по порядку, а не из слова.
    lines = ["КНИГА ПЕРВАЯ", "ГЛАВА ПЕРВАЯ", "a", "ГЛАВА ПШСТАЯ", "b",
             "ГЛАВА ТРЕТЬЯ", "c"]
    chapters = parse_pdf.parse_chapters(lines)
    assert [c.number for c in chapters] == [1, 2, 3]


def test_parse_chapters_rejects_ordinal_that_contradicts_order():
    # Заголовок читается, но стоит не там: значит потерян или лишний заголовок.
    lines = ["КНИГА ПЕРВАЯ", "ГЛАВА ПЕРВАЯ", "a", "ГЛАВА ТРЕТЬЯ", "b"]
    with pytest.raises(parse_pdf.HeadingError, match="ТРЕТЬЯ"):
        parse_pdf.parse_chapters(lines)


def test_parse_chapters_accepts_unknown_ordinal_by_order():
    # Нечитаемое порядковое слово — обычное дело после OCR: номер даёт порядок.
    lines = ["КНИГА ПЕРВАЯ (А)", "ГЛАВА БЕЗЫМЯННАЯ", "текст"]
    assert [(c.book, c.number) for c in parse_pdf.parse_chapters(lines)] == [("i", 1)]


def test_parse_chapters_supports_numbered_paragraph_style():
    # В томе 4 глав «ГЛАВА» нет: глава начинается номером с точкой и сразу
    # текстом — «1. Собираясь говорить о вопросах этики...».
    lines = ["КНИГА ПЕРВАЯ (А)", "1. Собираясь говорить о вопросах этики,", "продолжение.",
             "2. Далее речь пойдёт о добродетели."]
    chapters = parse_pdf.parse_chapters(lines, style="numbered")
    assert [(c.book, c.number) for c in chapters] == [("i", 1), ("i", 2)]
    assert chapters[0].text == "Собираясь говорить о вопросах этики,\nпродолжение."


def test_numbered_style_reads_cyrillic_lookalikes():
    # Tesseract читает номер главы кириллицей: «З.» — это 3, «И.» — это 11.
    lines = ["КНИГА ПЕРВАЯ (А)", "1. Первая глава.", "2. Вторая глава.",
             "З. Наряду с этим существует и другое деление.",
             "4. Четвёртая глава."]
    chapters = parse_pdf.parse_chapters(lines, style="numbered")
    assert [c.number for c in chapters] == [1, 2, 3, 4]
    assert chapters[2].text.startswith("Наряду с этим")


def test_parse_chapters_ne_style_uses_parenthesized_roman():
    # НЭ: глава — римский номер в скобках; арабский перед ним — счётчик
    # абзацев, его надо отбросить.
    lines = [
        "КНИГА ПЕРВАЯ (А)",
        "1 (I)· Всякое искусство и всякое учение.",
        "продолжение.",
        "(II). Если же у того, что мы делаем.",
        "(III). Рассуждение будет удовлетворительным.",
        "2 (IV). Поскольку всякое познание.",
        "КНИГА ВТОРАЯ (В)",
        "1(1). Итак, при наличии добродетели.",
    ]
    chapters = parse_pdf.parse_chapters(lines, style="ne")
    assert [(c.book, c.number) for c in chapters] == [
        ("i", 1), ("i", 2), ("i", 3), ("i", 4), ("ii", 1)
    ]
    assert chapters[0].text.startswith("Всякое искусство")
    assert chapters[3].text.startswith("Поскольку всякое")


def test_parse_chapters_ne_style_reads_ocr_lookalikes():
    # OCR: «(11)» = II, «(ХШ)» = XIII, перед маркером может стоять беккер.
    lines = [
        "КНИГА ПЕРВАЯ (А)",
        "Ю94а (I)· Всякое искусство.",
        "1 (11). Поскольку всякое познание.",
        "(111). Рассуждение.",
        "15 (ХШ). Итак, сказано.",
    ]
    chapters = parse_pdf.parse_chapters(lines, style="ne")
    assert [c.number for c in chapters] == [1, 2, 3, 13]


def test_parse_chapters_politics_style_reads_roman_chapters():
    # «Политика»: римская глава, сразу за ней номер абзаца 1.
    # OCR иногда читает «II 1.» как «I I I .».
    lines = [
        "КНИГА ПЕРВАЯ (А)",
        "I 1. Поскольку, как мы видим, всякое государство.",
        "продолжение.",
        "I I I . Уяснив, из каких элементов состоит.",
        "III 1. Теперь мы займёмся собственностью.",
        "IV 1. Осталось рассмотреть.",
        "КНИГА ВТОРАЯ (В)",
        "I 1. Так как мы ставим своей задачей.",
    ]
    chapters = parse_pdf.parse_chapters(lines, style="politics")
    assert [(c.book, c.number) for c in chapters] == [
        ("i", 1), ("i", 2), ("i", 3), ("i", 4), ("ii", 1)
    ]
    assert chapters[0].text.startswith("Поскольку")
    assert chapters[1].text.startswith("Уяснив")


# --- путь и ссылка в формате корпуса --------------------------------------

def test_chapter_path_mirrors_english_layout():
    assert parse_pdf.chapter_path("metaphysics", "ii", 6) == \
        Path("metaphysics") / "ii" / "06.md"


def test_cite_uses_russian_title_and_numbering():
    assert parse_pdf.cite("Метафизика", "ii", 6) == "Метафизика II.6"
    assert parse_pdf.cite("Метафизика", None, 3) == "Метафизика 3"
