from pathlib import Path
import pytest
from registry import Work
import sources

ROOT_SOURCES = Path(__file__).parent.parent.parent / "SOURCES.md"
TABLE = "| a |\n|---|\n| row |\n"

WORKS = {
    "nicomachean-ethics": Work(
        "nicomachean-ethics", "Nicomachean Ethics", "ethics", "genuine",
        "mit", "https://classics.mit.edu/Aristotle/nicomachaen.mb.txt",
        "W. D. Ross", "1925"),
    "problems": Work(
        "problems", "Problems", "biology", "spurious",
        "wikisource", "https://en.wikisource.org/wiki/Problems", "E. S. Forster",
        "1937"),
}


def test_lists_every_work():
    out = sources.render(WORKS, {"nicomachean-ethics": 10, "problems": 38})
    assert "Nicomachean Ethics" in out
    assert "W. D. Ross" in out
    assert "Problems" in out


def test_marks_spurious():
    out = sources.render(WORKS, {"nicomachean-ethics": 10, "problems": 38})
    problems_row = [l for l in out.splitlines() if "Problems" in l][0]
    assert "spurious" in problems_row


def test_render_is_table_only():
    out = sources.render(WORKS, {"nicomachean-ethics": 10, "problems": 38})
    assert out.startswith("| Работа |")
    assert "общественном достоянии" not in out


def test_table_has_year_column():
    out = sources.render(WORKS, {"nicomachean-ethics": 10, "problems": 38})
    header = [l for l in out.splitlines() if l.startswith("| Работа")][0]
    assert "Год" in header
    ne_row = [l for l in out.splitlines() if "Nicomachean Ethics" in l][0]
    assert "1925" in ne_row


def test_year_missing_renders_placeholder_not_blank():
    works = {"problems": Work(
        "problems", "Problems", "biology", "spurious",
        "wikisource", "https://en.wikisource.org/wiki/Problems",
        "E. S. Forster", None)}
    out = sources.render(works, {"problems": 38})
    row = [l for l in out.splitlines() if "Problems" in l][0]
    assert "—" in row


def test_splice_replaces_only_between_markers():
    before = ("# Заголовок\n\nпроза сверху\n\n"
              f"{sources.BEGIN}\n| old |\n{sources.END}\n\n## Ниже\nпроза снизу\n")
    after = sources.splice(before, TABLE, "не нужен")
    assert after == ("# Заголовок\n\nпроза сверху\n\n"
                     f"{sources.BEGIN}\n{TABLE}{sources.END}\n\n## Ниже\nпроза снизу\n")


def test_splice_refuses_without_markers():
    with pytest.raises(sources.SourcesError, match="меток"):
        sources.splice("# руками написанный файл\n", TABLE, "x")


@pytest.mark.parametrize("text", [
    f"{sources.BEGIN}\n",
    f"{sources.END}\n{sources.BEGIN}\n",
    f"{sources.BEGIN}\n{sources.END}\n{sources.BEGIN}\n{sources.END}\n",
])
def test_splice_refuses_broken_markers(text):
    with pytest.raises(sources.SourcesError):
        sources.splice(text, TABLE, "x")


def test_splice_creates_new_file():
    out = sources.splice(None, TABLE, "Источники корпуса: Эпиктет")
    assert out == (f"# Источники корпуса: Эпиктет\n\n{sources.BEGIN}\n{TABLE}{sources.END}\n")


# Обоснование прав переехало из кода в сам файл: сторожим его там.
def test_root_sources_keeps_rights_and_web_sections():
    text = ROOT_SOURCES.read_text(encoding="utf-8")
    assert "общественном достоянии" in text
    assert "вёрстк" in text
    assert "1 января 2027" in text and "On the Soul" in text
    assert "## Веб-интерфейс" in text
    assert text.count(sources.BEGIN) == 1 and text.count(sources.END) == 1
    assert text.index(sources.BEGIN) < text.index("| Работа |") < text.index(sources.END)
    assert text.index(sources.END) < text.index("## Веб-интерфейс")
