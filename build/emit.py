from pathlib import Path

from parse_mit import Chapter
from registry import Work


def chapter_path(work: Work, chapter: Chapter) -> Path:
    name = f"{chapter.number:02d}.md"
    if chapter.book is None:
        return Path(work.slug) / name
    return Path(work.slug) / chapter.book.lower() / name


def cite(work: Work, chapter: Chapter) -> str:
    if chapter.book is None:
        return f"{work.title} {chapter.number}"
    return f"{work.title} {chapter.book}.{chapter.number}"


def _yaml_str(value: str) -> str:
    return '"' + value.replace('\\', '\\\\').replace('"', '\\"') + '"'


def render(work: Work, chapter: Chapter, translator: str | None) -> str:
    tr = work.translator or translator
    book = _yaml_str(chapter.book) if chapter.book else "null"
    lines = [
        "---",
        f"work: {_yaml_str(work.title)}",
        f"work_slug: {work.slug}",
        f"book: {book}",
        f"chapter: {chapter.number}",
        f"group: {work.group}",
        f"authenticity: {work.authenticity}",
        f"translator: {_yaml_str(tr) if tr else 'null'}",
        f"source: {work.source}",
        f"cite: {_yaml_str(cite(work, chapter))}",
        "---",
        "",
        chapter.text.rstrip(),
        "",
    ]
    return "\n".join(lines)


def write(root: Path, work: Work, chapter: Chapter,
          translator: str | None) -> Path:
    path = Path(root) / chapter_path(work, chapter)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(render(work, chapter, translator), encoding="utf-8")
    return path
