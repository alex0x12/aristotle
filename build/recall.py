"""Recall поиска по контрольным вопросам: находит ли qmd ожидаемую главу.
Без модели и без ключа API.

Запуск: python recall.py [--philosopher SLUG] [-k 6]

Повторяет поиск клиента (internal/corpus/search.go): тот же вызов qmd —
флаги первыми, запрос последним после «--», — и та же отбраковка: подложные
главы и, если у вопроса есть group, главы чужой группы. Поэтому для Аристотеля
число совпадает с make eval-recall, а для пакета, который Go-клиент ещё не
умеет открывать, это единственный замер поиска."""
import argparse
import json
import subprocess
import sys
from pathlib import Path

import yaml

from target import resolve

HERE = Path(__file__).parent
ROOT = HERE.parent


def run_qmd(*args: str) -> str:
    done = subprocess.run(["qmd", *args], capture_output=True, text=True,
                          check=True, timeout=300)
    return done.stdout


def _frontmatter(text: str) -> str:
    """Шапка главы без тела: поля ищем только в ней, а не в тексте перевода."""
    return text.split("\n---\n", 1)[0] + "\n"


def search(collection: str, corpus: Path, query: str, k: int, run=run_qmd,
           group: str = "") -> list[str]:
    out = run("query", "-c", collection, "--json", "-n", str(k),
              "--no-rerank", "--", query)
    prefix = f"qmd://{collection}/"
    found = []
    for r in json.loads(out):
        rel = r["file"].removeprefix(prefix)
        path = Path(corpus) / rel
        if not path.is_file():
            continue                     # индекс отстал от диска
        head = _frontmatter(path.read_text(encoding="utf-8"))
        if "\nauthenticity: spurious\n" in head:
            continue                     # клиент подложное не показывает — и мы не считаем
        if group and f"\ngroup: {group}\n" not in head:
            continue                     # вопрос сужен до группы, как у клиента
        found.append(rel)
    return found


def main(argv=None, run=run_qmd, out=sys.stdout, root: Path = ROOT) -> int:
    p = argparse.ArgumentParser(description="Recall поиска по контрольным вопросам")
    p.add_argument("--philosopher", metavar="SLUG",
                   help="пакет philosophers/SLUG вместо Аристотеля")
    p.add_argument("-k", type=int, default=6, help="сколько глав берёт поиск (как у клиента)")
    args = p.parse_args(argv)

    t = resolve(root, args.philosopher)
    items = yaml.safe_load(t.questions.read_text(encoding="utf-8")) or []
    questions = [q for q in items if q.get("expect")]      # ловушки оцениваются не поиском

    hits = 0
    for q in questions:
        found = search(t.collection, t.corpus, q["ask"], args.k, run, q.get("group", ""))
        ok = q["expect"] in found
        hits += ok
        print(f"{'+' if ok else '-'} {q['expect']:40s} {q['ask']}", file=out)
    print(f"\nrecall@{args.k}: {hits}/{len(questions)}", file=out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
