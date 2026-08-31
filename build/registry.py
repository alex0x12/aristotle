from dataclasses import dataclass
from pathlib import Path
import yaml

GROUPS = {
    "logic", "physics", "psychology", "biology",
    "metaphysics", "ethics", "politics", "rhetoric-poetics",
}
AUTHENTICITY = {"genuine", "disputed", "spurious"}
SOURCES = {"mit", "wikisource"}
REQUIRED = ("title", "group", "authenticity", "source", "url")


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


def load(path: Path) -> dict[str, Work]:
    raw = yaml.safe_load(Path(path).read_text(encoding="utf-8")) or {}
    works: dict[str, Work] = {}
    for slug, d in raw.items():
        for field in REQUIRED:
            if field not in d:
                raise RegistryError(f"{slug}: отсутствует поле {field}")
        if d["group"] not in GROUPS:
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
        )
    return works
