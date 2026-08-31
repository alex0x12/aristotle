from pathlib import Path
import emit
from registry import Work
from parse_mit import Chapter

WORK = Work(
    slug="nicomachean-ethics",
    title="Nicomachean Ethics",
    group="ethics",
    authenticity="genuine",
    source="mit",
    url="https://classics.mit.edu/Aristotle/nicomachaen.mb.txt",
    translator=None,
)
CH = Chapter(book="I", number=7, text="The good of man is an activity of soul.")


def test_path_with_book():
    assert emit.chapter_path(WORK, CH) == Path("nicomachean-ethics/i/07.md")


def test_path_without_book():
    ch = Chapter(book=None, number=6, text="tragedy is an imitation")
    work = Work("poetics", "Poetics", "rhetoric-poetics", "genuine", "mit", "u")
    assert emit.chapter_path(work, ch) == Path("poetics/06.md")


def test_render_frontmatter():
    out = emit.render(WORK, CH, "W. D. Ross")
    assert out.startswith("---\n")
    assert 'work: "Nicomachean Ethics"' in out
    assert "work_slug: nicomachean-ethics" in out
    assert 'book: "I"' in out
    assert "chapter: 7" in out
    assert "group: ethics" in out
    assert "authenticity: genuine" in out
    assert 'translator: "W. D. Ross"' in out
    assert 'cite: "Nicomachean Ethics I.7"' in out
    assert out.rstrip().endswith("The good of man is an activity of soul.")


def test_render_cite_without_book():
    ch = Chapter(book=None, number=6, text="tragedy is an imitation")
    work = Work("poetics", "Poetics", "rhetoric-poetics", "genuine", "mit", "u")
    out = emit.render(work, ch, "S. H. Butcher")
    assert 'cite: "Poetics 6"' in out
    assert "book: null" in out


def test_write_creates_file(tmp_path):
    path = emit.write(tmp_path, WORK, CH, "W. D. Ross")
    assert path.exists()
    assert path.relative_to(tmp_path) == Path("nicomachean-ethics/i/07.md")
