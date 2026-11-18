"""Таблица состава корпуса для SOURCES.md.

Сборка владеет только таблицей: она стоит между двумя строками-метками, и
пересборка заменяет то, что между ними. Всё остальное в файле — обоснование
прав, раздел о картинках и шрифтах — пишут люди, и сборка его не трогает."""
from registry import Work

BEGIN = "<!-- corpus-table:begin -->"
END = "<!-- corpus-table:end -->"

HEADER = ("| Работа | Переводчик | Год | Подлинность | Глав | Источник |\n"
          "|---|---|---|---|---|---|\n")


class SourcesError(Exception):
    pass


def render(works: dict[str, Work], counts: dict[str, int]) -> str:
    rows = []
    for slug, w in sorted(works.items(), key=lambda kv: kv[1].title):
        rows.append(
            f"| [{w.title}]({w.url}) | {w.translator or '—'} | {w.year or '—'} "
            f"| {w.authenticity} | {counts.get(slug, 0)} | {w.source} |")
    return HEADER + "\n".join(rows) + "\n"


def check_markers(existing: str | None, path) -> None:
    """Отказ, если в существующем файле нет ровно одной пары меток по порядку.
    Переписать файл целиком взамен — значит молча стереть написанное руками."""
    if existing is None:
        return
    if (existing.count(BEGIN) != 1 or existing.count(END) != 1
            or existing.index(BEGIN) > existing.index(END)):
        raise SourcesError(
            f"{path}: нет пары меток {BEGIN} … {END} — таблицу некуда вставить, "
            "а весь файл сборка не переписывает")


def splice(existing: str | None, table: str, title: str) -> str:
    if existing is None:
        return f"# {title}\n\n{BEGIN}\n{table}{END}\n"
    check_markers(existing, "SOURCES.md")
    head, rest = existing.split(BEGIN, 1)
    _, tail = rest.split(END, 1)
    return f"{head}{BEGIN}\n{table}{END}{tail}"
