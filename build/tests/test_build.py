"""Сборка пакета на фикстуре: одна работа «MIT», текст лежит в кэше заранее,
поэтому сеть не нужна."""
from pathlib import Path
import shutil
import pytest

import build
import fetch
import sources
from target import resolve

FIXTURES = Path(__file__).parent / "fixtures"
URL = "https://classics.mit.edu/Aristotle/demo.mb.txt"


def make_package(root: Path, cache: Path) -> Path:
    pkg = root / "philosophers" / "demo"
    pkg.mkdir(parents=True)
    (pkg / "philosopher.yaml").write_text(
        "name: Демо\nname_en: Demo\ngroups: [ethics]\n", encoding="utf-8")
    (pkg / "works.yaml").write_text(
        "demo-work:\n"
        "  title: Demo Work\n"
        "  group: ethics\n"
        "  authenticity: genuine\n"
        "  source: mit\n"
        f"  url: {URL}\n"
        "  year: 1900\n", encoding="utf-8")
    cache.mkdir(parents=True, exist_ok=True)
    shutil.copy(FIXTURES / "mit_books.mb.txt", fetch._cache_path(URL, cache))
    return pkg


def test_package_build_writes_only_inside_package(tmp_path):
    cache = tmp_path / "cache"
    pkg = make_package(tmp_path, cache)
    assert build.run(resolve(tmp_path, "demo"), cache, force=False) == 0

    chapters = sorted((pkg / "corpus").rglob("*.md"))
    assert chapters, "главы должны появиться в пакете"
    first = chapters[0].read_text(encoding="utf-8")
    assert "work_slug: demo-work" in first and "group: ethics" in first
    assert not (tmp_path / "corpus").exists()
    assert not (tmp_path / "SOURCES.md").exists()

    text = (pkg / "SOURCES.md").read_text(encoding="utf-8")
    assert text.startswith("# Источники корпуса: Демо\n")
    assert sources.BEGIN in text and "Demo Work" in text


def test_rebuild_keeps_prose(tmp_path):
    cache = tmp_path / "cache"
    pkg = make_package(tmp_path, cache)
    build.run(resolve(tmp_path, "demo"), cache, force=False)
    md = pkg / "SOURCES.md"
    md.write_text(md.read_text(encoding="utf-8") + "\n## Права\nнаписано руками\n",
                  encoding="utf-8")
    build.run(resolve(tmp_path, "demo"), cache, force=False)
    assert "написано руками" in md.read_text(encoding="utf-8")


def test_no_markers_refuses_before_writing_anything(tmp_path):
    cache = tmp_path / "cache"
    pkg = make_package(tmp_path, cache)
    (pkg / "SOURCES.md").write_text("# руками, без меток\n", encoding="utf-8")
    with pytest.raises(sources.SourcesError):
        build.run(resolve(tmp_path, "demo"), cache, force=False)
    assert not (pkg / "corpus").exists(), "корпус не должен меняться, если таблицу некуда вставить"
    assert (pkg / "SOURCES.md").read_text(encoding="utf-8") == "# руками, без меток\n"


def test_group_outside_manifest_rejected(tmp_path):
    cache = tmp_path / "cache"
    pkg = make_package(tmp_path, cache)
    (pkg / "philosopher.yaml").write_text(
        "name: Демо\nname_en: Demo\ngroups: [logic]\n", encoding="utf-8")
    with pytest.raises(Exception, match="group"):
        build.run(resolve(tmp_path, "demo"), cache, force=False)
