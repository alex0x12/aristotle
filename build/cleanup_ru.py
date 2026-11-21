"""Поиск систематических ошибок OCR по частотам самого корпуса.

Словаря русского языка в системе нет, поэтому опора — на сам корпус: если
слово встречается много раз в одном написании и единицы раз в другом,
отличающемся на одну «похожую» букву, редкое написание — кандидат на замену.
По умолчанию ничего не пишется; правки применяются только с ``--fix`` и только
там, где замена однозначна.

Запуск:
    venv/bin/python cleanup_ru.py --report ../corpus-ru
    venv/bin/python cleanup_ru.py --report ../corpus-ru --fix
"""
import argparse
import re
from collections import Counter
from pathlib import Path

# Буквы, которые OCR путает между собой: кириллица н/п/и, греческая π,
# латиница, похожая на кириллицу. Замена всегда однобуквенная.
CONFUSABLE = {
    # н/п/и и греческая π — главные ошибки распознавания русских букв.
    "н": ("п", "h"),
    "п": ("н", "и", "π"),
    "и": ("п", "π"),
    "π": ("и", "п", "н"),
    # латиница внутри русского слова — только одноимённые кириллические
    # буквы; гласные а/о/е между собой НЕ меняем: это обычно не OCR, а
    # другое окончание или падеж.
    "а": ("a",), "о": ("o",), "е": ("e",), "с": ("c",), "р": ("p",),
    "х": ("x",), "у": ("y",), "к": ("k",), "т": ("t",), "в": ("b",),
    "м": ("m",), "і": ("i", "l"),
    "a": ("а",), "o": ("о",), "e": ("е",), "c": ("с",), "p": ("р",),
    "x": ("х",), "y": ("у",), "k": ("к",), "t": ("т",), "h": ("н",),
    "b": ("в",), "m": ("м",), "i": ("і",), "l": ("і",),
}

WORD_CHARS = "А-Яа-яЁёA-Za-zπ"
WORD_RE = re.compile(rf"[{WORD_CHARS}]+")
MIN_LEN = 4          # короткие слова слишком легко «исправить» не туда
MAX_BAD = 2          # искажённая форма должна быть редкой
MIN_GOOD = 5         # правильная — частой
RATIO = 3            # и заметно чаще искажённой


def _variants(word: str):
    """Все однобуквенные замены слова на «похожие» буквы."""
    yield word
    for i, ch in enumerate(word):
        for repl in CONFUSABLE.get(ch, ()):
            yield word[:i] + repl + word[i + 1:]


def suggest(text: str) -> list[tuple[str, str, int]]:
    """Кандидаты на замену: ``[(искажённое, правильное, сколько_раз)]``."""
    counts = Counter(w.lower() for w in WORD_RE.findall(text))
    best: dict[str, tuple[str, str, int]] = {}
    for wrong, bad_count in counts.items():
        if bad_count > MAX_BAD or len(wrong) < MIN_LEN:
            continue
        for variant in _variants(wrong):
            if variant == wrong:
                continue
            good_count = counts.get(variant, 0)
            if (good_count < MIN_GOOD or good_count < RATIO * bad_count):
                continue
            prev = best.get(wrong)
            if prev is None or good_count > counts[prev[1]]:
                best[wrong] = (wrong, variant, bad_count)
    # Не предлагаем замену, если «правильная» форма сама попала в кандидаты
    # или выглядит искажённой: иначе получится цепочка
    # «бескопечпое -> бесконечпое -> бесконечное».
    wrongs = set(best)
    return sorted(
        (item for item in best.values()
         if item[1] not in wrongs and not _suspicious(item[1], counts)),
        key=lambda item: (-item[2], item[0]))


def _suspicious(word: str, counts: Counter) -> bool:
    """Слово само похоже на OCR-ошибку: рядом есть заметно более частое."""
    own = counts[word]
    for variant in _variants(word):
        if variant != word and counts[variant] >= max(20, RATIO * own):
            return True
    return False


def _replace_word(match: re.Match, right: str) -> str:
    word = match.group(0)
    if word.isupper():
        return right.upper()
    if word[:1].isupper():
        return right[:1].upper() + right[1:]
    return right


def apply_fixes(text: str, candidates) -> str:
    """Заменяет целые слова, сохраняя регистр; границы — не буквы."""
    for wrong, right, _ in candidates:
        pattern = re.compile(
            rf"(?<![{WORD_CHARS}]){re.escape(wrong)}(?![{WORD_CHARS}])",
            re.IGNORECASE)
        text = pattern.sub(lambda m, r=right: _replace_word(m, r), text)
    return text


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description="Чистка OCR по частотам корпуса")
    parser.add_argument("--report", required=True,
                        help="каталог корпуса (рекурсивно ищутся *.md)")
    parser.add_argument("--fix", action="store_true",
                        help="применить однозначные замены")
    args = parser.parse_args(argv)

    root = Path(args.report)
    files = sorted(root.rglob("*.md"))
    if not files:
        print(f"в {root} нет файлов *.md")
        return 0

    text = "\n".join(path.read_text(encoding="utf-8") for path in files)
    candidates = suggest(text)

    if args.fix:
        for path in files:
            original = path.read_text(encoding="utf-8")
            fixed = apply_fixes(original, candidates)
            if fixed != original:
                path.write_text(fixed, encoding="utf-8")

    print(f"файлов: {len(files)}, кандидатов: {len(candidates)}")
    for wrong, right, count in candidates[:50]:
        print(f"{count:4d}  {wrong} -> {right}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
