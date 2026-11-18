from dataclasses import dataclass
from pathlib import Path
import re
import yaml

GROUPS = {
    "logic", "physics", "psychology", "biology",
    "metaphysics", "ethics", "politics", "rhetoric-poetics",
}
AUTHENTICITY = {"genuine", "disputed", "spurious"}
SOURCES = {"mit", "wikisource"}
REQUIRED = ("title", "group", "authenticity", "source", "url")
GROUP_RE = re.compile(r"^[a-z]+(-[a-z]+)*$")


class RegistryError(Exception):
    pass


@dataclass(frozen=True)
class Work:
    slug: str
    title: str
    group: str
    authenticity: str
    source: str
    url: str
    translator: str | None = None
    # Год первой публикации именно этого перевода (не год написания
    # Аристотелем, не год позднейшего переиздания или включения в
    # многотомник). Не входит в REQUIRED: старые фикстуры и тесты, где год
    # роли не играет, не обязаны его указывать. Строка, а не число — на
    # случай, если для работы год достоверно не устанавливается и поле
    # несёт пояснительный текст вместо цифры (см. build/works.yaml).
    year: str | None = None


def load(path: Path, groups: frozenset[str] | set[str] = GROUPS) -> dict[str, Work]:
    raw = yaml.safe_load(Path(path).read_text(encoding="utf-8")) or {}
    works: dict[str, Work] = {}
    for slug, d in raw.items():
        for field in REQUIRED:
            if field not in d:
                raise RegistryError(f"{slug}: отсутствует поле {field}")
        if d["group"] not in groups:
            raise RegistryError(f"{slug}: недопустимый group {d['group']!r}")
        if d["authenticity"] not in AUTHENTICITY:
            raise RegistryError(
                f"{slug}: недопустимый authenticity {d['authenticity']!r}")
        if d["source"] not in SOURCES:
            raise RegistryError(f"{slug}: недопустимый source {d['source']!r}")
        works[slug] = Work(
            slug=slug,
            title=d["title"],
            group=d["group"],
            authenticity=d["authenticity"],
            source=d["source"],
            url=d["url"],
            translator=d.get("translator"),
            year=str(d["year"]) if d.get("year") is not None else None,
        )
    return works


@dataclass(frozen=True)
class Manifest:
    """Манифест пакета философа. Конвейер читает только groups — допустимые
    значения поля group в works.yaml; имя и годы нужны интерфейсу библиотеки."""
    name: str
    name_en: str
    groups: frozenset[str]
    years: str | None = None


def load_manifest(path: Path) -> Manifest:
    path = Path(path)
    if not path.exists():
        raise RegistryError(f"нет манифеста {path}")
    d = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    for field in ("name", "name_en", "groups"):
        if field not in d:
            raise RegistryError(f"{path.name}: отсутствует поле {field}")
    groups = d["groups"]
    if (not isinstance(groups, list) or not groups
            or not all(isinstance(g, str) and GROUP_RE.match(g) for g in groups)):
        raise RegistryError(
            f"{path.name}: groups — непустой список имён из строчной латиницы через дефис")
    years = d.get("years")
    return Manifest(name=str(d["name"]), name_en=str(d["name_en"]),
                    groups=frozenset(groups),
                    years=str(years) if years is not None else None)
