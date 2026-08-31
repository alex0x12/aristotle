from registry import Work
import sources

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


def test_states_public_domain_reasoning():
    out = sources.render(WORKS, {})
    assert "общественном достоянии" in out
    assert "вёрстк" in out, "должно быть сказано, что вёрстка не воспроизводится"


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


def test_states_1931_caveat_for_on_the_soul():
    out = sources.render(WORKS, {})
    assert "1931" in out
    assert "On the Soul" in out
    assert "1 января 2027" in out, \
        "95 лет с 1931 года истекают в 2026-м, дата перехода в PD — начало следующего года"
