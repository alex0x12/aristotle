from pathlib import Path
import pytest
from registry import load, Work, RegistryError

FIXTURE = Path(__file__).parent / "fixtures" / "works_ok.yaml"

def test_loads_work():
    works = load(FIXTURE)
    ne = works["nicomachean-ethics"]
    assert ne == Work(
        slug="nicomachean-ethics",
        title="Nicomachean Ethics",
        group="ethics",
        authenticity="genuine",
        source="mit",
        url="https://classics.mit.edu/Aristotle/nicomachaen.mb.txt",
        translator=None,
    )

def test_rejects_unknown_group(tmp_path):
    bad = tmp_path / "bad.yaml"
    bad.write_text(
        "poetics:\n"
        "  title: Poetics\n"
        "  group: poesy\n"
        "  authenticity: genuine\n"
        "  source: mit\n"
        "  url: https://classics.mit.edu/Aristotle/poetics.mb.txt\n",
        encoding="utf-8",
    )
    with pytest.raises(RegistryError, match="group"):
        load(bad)

def test_rejects_missing_field(tmp_path):
    bad = tmp_path / "bad.yaml"
    bad.write_text(
        "poetics:\n"
        "  title: Poetics\n"
        "  group: rhetoric-poetics\n",
        encoding="utf-8",
    )
    with pytest.raises(RegistryError, match="authenticity"):
        load(bad)

WORKS = Path(__file__).parent.parent / "works.yaml"

def test_production_registry_loads():
    works = load(WORKS)
    assert len(works) == 29
    assert works["nicomachean-ethics"].group == "ethics"
    assert all(w.url.startswith("https://") for w in works.values())
    mit = [w for w in works.values() if w.source == "mit"]
    ws = [w for w in works.values() if w.source == "wikisource"]
    assert len(mit) == 20
    assert len(ws) == 9
    assert all(w.authenticity == "genuine" for w in works.values()), \
        "спурия недостижима ни из одного источника, см. преамбулу задачи"
    assert all(w.url.endswith(".mb.txt") for w in mit)
    assert all(w.translator for w in ws), "у работ Wikisource переводчик задан явно"
