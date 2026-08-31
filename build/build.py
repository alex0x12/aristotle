"""Сборка корпуса: реестр -> corpus/. Запуск: python build.py [--force]"""
import shutil
import sys
from pathlib import Path

import emit
import fetch
import parse_mit
import registry

HERE = Path(__file__).parent
ROOT = HERE.parent
CACHE = HERE / ".cache"
CORPUS = ROOT / "corpus"

PARSERS = {"mit": parse_mit.parse}


def main() -> int:
    works = registry.load(HERE / "works.yaml")
    if "--force" in sys.argv and CORPUS.exists():
        shutil.rmtree(CORPUS)

    total = 0
    for slug, work in sorted(works.items()):
        parser = PARSERS.get(work.source)
        if parser is None:
            print(f"{slug}: нет парсера для источника {work.source}, пропуск")
            continue
        raw = fetch.fetch(work.url, CACHE)
        translator, chapters = parser(raw)
        for chapter in chapters:
            emit.write(CORPUS, work, chapter, translator)
        total += len(chapters)
        print(f"{slug:34s} {len(chapters):4d} глав")

    print(f"\nвсего глав: {total}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
