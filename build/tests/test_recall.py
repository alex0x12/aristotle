import io
import json
from pathlib import Path

import recall


def chapter(root: Path, rel: str, authenticity="genuine", group="ethics"):
    p = root / rel
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(f"---\nwork: W\ngroup: {group}\nauthenticity: {authenticity}\n---\n\ntext\n",
                 encoding="utf-8")


def fake_qmd(results, calls):
    def run(*args):
        calls.append(args)
        return json.dumps([{"file": f, "score": 0.5} for f in results])
    return run


def test_query_argument_order(tmp_path):
    calls = []
    recall.search("demo", tmp_path, "-c чужая", 6, fake_qmd([], calls))
    # Флаги первыми, запрос последним после «--»: иначе qmd прочёл бы его как флаг.
    # --no-rerank — до «--», как и у Go-клиента: реранкер дороже всего поиска.
    assert calls == [("query", "-c", "demo", "--json", "-n", "6",
                      "--no-rerank", "--", "-c чужая")]


def test_paths_relative_and_spurious_dropped(tmp_path):
    chapter(tmp_path, "ethics/i/01.md")
    chapter(tmp_path, "fake/01.md", authenticity="spurious")
    found = recall.search("demo", tmp_path, "q", 6, fake_qmd(
        ["qmd://demo/ethics/i/01.md", "qmd://demo/fake/01.md", "qmd://demo/gone/01.md"], []))
    assert found == ["ethics/i/01.md"]


def test_group_filters_hits(tmp_path):
    chapter(tmp_path, "ethics/i/01.md", group="ethics")
    chapter(tmp_path, "logic/01.md", group="logic")
    both = ["qmd://demo/ethics/i/01.md", "qmd://demo/logic/01.md"]
    assert recall.search("demo", tmp_path, "q", 6, fake_qmd(both, [])) == [
        "ethics/i/01.md", "logic/01.md"]
    assert recall.search("demo", tmp_path, "q", 6, fake_qmd(both, []),
                         group="logic") == ["logic/01.md"]


def test_main_counts_hits_and_skips_traps(tmp_path):
    pkg = tmp_path / "philosophers" / "demo"
    (pkg / "eval").mkdir(parents=True)
    (pkg / "philosopher.yaml").write_text(
        "name: Демо\nname_en: Demo\ngroups: [ethics]\n", encoding="utf-8")
    (pkg / "eval" / "questions.yaml").write_text(
        "- ask: first\n  expect: a/01.md\n"
        "- ask: second\n  expect: b/01.md\n"
        "- ask: ловушка\n  note: без адреса\n", encoding="utf-8")
    chapter(pkg / "corpus", "a/01.md")
    calls = []
    out = io.StringIO()
    rc = recall.main(["--philosopher", "demo"], run=fake_qmd(["qmd://demo/a/01.md"], calls),
                     out=out, root=tmp_path)
    assert rc == 0
    assert len(calls) == 2                       # ловушка без expect в поиск не идёт
    assert all(c[2] == "demo" for c in calls)
    assert "recall@6: 1/2" in out.getvalue()


def test_main_honours_question_group(tmp_path):
    (tmp_path / "eval").mkdir()
    (tmp_path / "eval" / "questions.yaml").write_text(
        "- ask: q\n  expect: a/01.md\n  group: logic\n", encoding="utf-8")
    chapter(tmp_path / "corpus", "a/01.md", group="ethics")
    out = io.StringIO()
    recall.main([], run=fake_qmd(["qmd://aristotle/a/01.md"], []), out=out, root=tmp_path)
    assert "recall@6: 0/1" in out.getvalue()      # глава есть, но не той группы


def test_aristotle_collection_by_default(tmp_path):
    (tmp_path / "eval").mkdir()
    (tmp_path / "eval" / "questions.yaml").write_text(
        "- ask: q\n  expect: x/01.md\n", encoding="utf-8")
    calls = []
    recall.main([], run=fake_qmd([], calls), out=io.StringIO(), root=tmp_path)
    assert calls[0][2] == "aristotle"
