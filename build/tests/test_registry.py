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

def test_loads_year_field(tmp_path):
    y = tmp_path / "y.yaml"
    y.write_text(
        "poetics:\n"
        "  title: Poetics\n"
        "  group: rhetoric-poetics\n"
        "  authenticity: genuine\n"
        "  source: mit\n"
        "  url: https://classics.mit.edu/Aristotle/poetics.mb.txt\n"
        "  year: 1895\n",
        encoding="utf-8",
    )
    works = load(y)
    assert works["poetics"].year == "1895"


def test_year_field_absent_is_none():
    works = load(FIXTURE)
    assert works["nicomachean-ethics"].year is None


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


def test_production_registry_has_year_for_every_work():
    # Спека 7: правовой довод держится на годе издания перевода — без него
    # читатель не может проверить единственный факт, на котором основана
    # позиция по правам (см. SOURCES.md).
    works = load(WORKS)
    missing = [slug for slug, w in works.items() if not w.year]
    assert not missing, f"нет года издания у: {missing}"
    assert all(w.year.isdigit() for w in works.values()), \
        "для всех 29 работ год установлен как число; текстовая заглушка " \
        "('год не установлен') сейчас не используется ни для одной работы"
