"""Сборка корпуса: реестр -> corpus/. Запуск: python build.py [--force]"""
import dataclasses
import shutil
import sys
from pathlib import Path

import emit
import fetch
import parse_mit
import parse_ws
import registry
import sources

HERE = Path(__file__).parent
ROOT = HERE.parent
CACHE = HERE / ".cache"
CORPUS = ROOT / "corpus"


def _chapters(work, get):
    if work.source == "mit":
        return parse_mit.parse(fetch.fetch(work.url, CACHE))
    if work.source == "wikisource":
        return parse_ws.parse_work(work, get)
    raise ValueError(f"неизвестный источник {work.source}")


def main() -> int:
    works = registry.load(HERE / "works.yaml")
    if "--force" in sys.argv and CORPUS.exists():
        shutil.rmtree(CORPUS)

    get = lambda url: fetch.fetch(url, CACHE)

    total = 0
    counts: dict[str, int] = {}
    found: dict[str, str] = {}
    for slug, work in sorted(works.items()):
        translator, chapters = _chapters(work, get)
        for chapter in chapters:
            emit.write(CORPUS, work, chapter, translator)
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
    (ROOT / "SOURCES.md").write_text(
        sources.render(enriched, counts), encoding="utf-8")
    print("SOURCES.md записан")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
