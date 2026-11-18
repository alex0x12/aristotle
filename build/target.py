"""Где лежат части философа: реестр, корпус, SOURCES.md, вопросы, коллекция qmd.

Аристотель собран первым и лежит в корне репозитория; остальные философы —
пакетами в philosophers/<slug>/. Раскладку знает только этот модуль."""
import re
from dataclasses import dataclass
from pathlib import Path

import registry

SLUG_RE = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")


@dataclass(frozen=True)
class Target:
    registry: Path
    corpus: Path
    sources_md: Path
    questions: Path
    groups: frozenset
    collection: str          # коллекция qmd
    title: str               # заголовок SOURCES.md, если файла ещё нет


def resolve(root: Path, philosopher: str | None) -> Target:
    root = Path(root)
    if philosopher is None:
        return Target(
            registry=root / "build" / "works.yaml",
            corpus=root / "corpus",
            sources_md=root / "SOURCES.md",
            questions=root / "eval" / "questions.yaml",
            groups=frozenset(registry.GROUPS),
            collection="aristotle",
            title="Источники корпуса",
        )
    if not SLUG_RE.match(philosopher):
        raise registry.RegistryError(
            f"недопустимое имя пакета {philosopher!r}: строчная латиница, цифры, дефис")
    pkg = root / "philosophers" / philosopher
    manifest = registry.load_manifest(pkg / "philosopher.yaml")
    return Target(
        registry=pkg / "works.yaml",
        corpus=pkg / "corpus",
        sources_md=pkg / "SOURCES.md",
        questions=pkg / "eval" / "questions.yaml",
        groups=manifest.groups,
        collection=philosopher,
        title=f"Источники корпуса: {manifest.name}",
    )
