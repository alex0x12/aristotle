from pathlib import Path
import pytest
from registry import GROUPS, RegistryError
from target import resolve


def test_aristotle_lives_at_root(tmp_path):
    t = resolve(tmp_path, None)
    assert t.registry == tmp_path / "build" / "works.yaml"
    assert t.corpus == tmp_path / "corpus"
    assert t.sources_md == tmp_path / "SOURCES.md"
    assert t.questions == tmp_path / "eval" / "questions.yaml"
    assert t.groups == frozenset(GROUPS)
    assert t.collection == "aristotle"


def test_package_paths_and_groups(tmp_path):
    pkg = tmp_path / "philosophers" / "epictetus"
    pkg.mkdir(parents=True)
    (pkg / "philosopher.yaml").write_text(
        "name: Эпиктет\nname_en: Epictetus\ngroups: [ethics]\n", encoding="utf-8")
    t = resolve(tmp_path, "epictetus")
    assert t.registry == pkg / "works.yaml"
    assert t.corpus == pkg / "corpus"
    assert t.sources_md == pkg / "SOURCES.md"
    assert t.questions == pkg / "eval" / "questions.yaml"
    assert t.groups == frozenset({"ethics"})
    assert t.collection == "epictetus"
    assert "Эпиктет" in t.title


@pytest.mark.parametrize("slug", ["Epictetus", "../x", "a_b", "", "-a"])
def test_bad_slug(tmp_path, slug):
    with pytest.raises(RegistryError, match="пакета"):
        resolve(tmp_path, slug)
