"""Сборка корпуса: реестр -> корпус и таблица в SOURCES.md.

Запуск: python build.py [--force] [--philosopher SLUG]
Без --philosopher собирается Аристотель в корне репозитория, как раньше."""
import argparse
import dataclasses
import shutil
from pathlib import Path

import emit
import fetch
import parse_mit
import parse_ws
import registry
import sources
from target import Target, resolve

HERE = Path(__file__).parent
ROOT = HERE.parent
CACHE = HERE / ".cache"


def _chapters(work, get, cache):
    if work.source == "mit":
        return parse_mit.parse(fetch.fetch(work.url, cache))
    if work.source == "wikisource":
        return parse_ws.parse_work(work, get)
    raise ValueError(f"неизвестный источник {work.source}")


def run(t: Target, cache: Path, force: bool) -> int:
    works = registry.load(t.registry, t.groups)

    # Таблицу должно быть куда вставить — проверяем ДО записи глав: отказ в
    # конце оставил бы корпус новым, а SOURCES.md старым.
    existing = (t.sources_md.read_text(encoding="utf-8")
                if t.sources_md.exists() else None)
    sources.check_markers(existing, t.sources_md)

    if force and t.corpus.exists():
        shutil.rmtree(t.corpus)

    get = lambda url: fetch.fetch(url, cache)

    total = 0
    counts: dict[str, int] = {}
    found: dict[str, str] = {}
    for slug, work in sorted(works.items()):
        translator, chapters = _chapters(work, get, cache)
        for chapter in chapters:
            emit.write(t.corpus, work, chapter, translator)
        counts[slug] = len(chapters)
        if translator:
            found[slug] = translator
        total += len(chapters)
        print(f"{slug:34s} {len(chapters):4d} глав")

    print(f"\nвсего глав: {total}")

    enriched = {
        slug: dataclasses.replace(w, translator=w.translator or found.get(slug))
        for slug, w in works.items()
    }
    t.sources_md.parent.mkdir(parents=True, exist_ok=True)
    t.sources_md.write_text(
        sources.splice(existing, sources.render(enriched, counts), t.title),
        encoding="utf-8")
    print(f"{t.sources_md}: таблица обновлена")
    return 0


def main(argv=None) -> int:
    p = argparse.ArgumentParser(description="Сборка корпуса из реестра")
    p.add_argument("--force", action="store_true",
                   help="удалить корпус и собрать заново")
    p.add_argument("--philosopher", metavar="SLUG",
                   help="собрать пакет philosophers/SLUG вместо Аристотеля")
    args = p.parse_args(argv)
    return run(resolve(ROOT, args.philosopher), CACHE, args.force)


if __name__ == "__main__":
    raise SystemExit(main())
