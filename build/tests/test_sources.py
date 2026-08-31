from registry import Work
import sources

WORKS = {
    "nicomachean-ethics": Work(
        "nicomachean-ethics", "Nicomachean Ethics", "ethics", "genuine",
        "mit", "https://classics.mit.edu/Aristotle/nicomachaen.mb.txt",
        "W. D. Ross"),
    "problems": Work(
        "problems", "Problems", "biology", "spurious",
        "wikisource", "https://en.wikisource.org/wiki/Problems", "E. S. Forster"),
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
