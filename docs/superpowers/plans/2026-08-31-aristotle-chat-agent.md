# Чат-агент «Аристотель» — план реализации

> **Для агентов-исполнителей:** ОБЯЗАТЕЛЬНЫЙ СУБ-НАВЫК: используйте
> superpowers:subagent-driven-development (рекомендуется) или
> superpowers:executing-plans для выполнения задача за задачей.
> Шаги размечены чекбоксами (`- [ ]`).

**Цель:** собеседник, отвечающий на вопросы исходя из реальной позиции Аристотеля,
с механически проверяемыми цитатами.

**Архитектура:** корпус Аристотеля разбирается из текстовых файлов MIT Internet
Classics Archive в markdown по главам и индексируется отдельной коллекцией qmd.
Go-клиент даёт модели инструмент поиска по этой коллекции; модель отвечает
в персоне, размечая выдержки и выводы маркерами, а клиент механически сверяет
каждую выдержку с тем, что поиск реально вернул.

**Технологии:** Python 3.13+ (сборка корпуса, разовая), Go 1.26 (агент),
qmd 2.8.3, Anthropic Go SDK, OpenAI-совместимые API.

**Спека:** `docs/superpowers/specs/2026-08-31-aristotle-chat-agent-design.md` —
читать перед началом. Номера разделов ниже ссылаются на неё.

## Глобальные ограничения

- **Никаких эмодзи в выводе CLI.** На машине нет эмодзи-шрифта. Только ASCII
  и рамочные символы Unicode (`│ ─ ┌`), которые несёт JetBrainsMono Nerd Font.
- **Модель по умолчанию — `claude-opus-5`**, строкой, типизированной константы
  в Go SDK нет.
- **`MaxTokens` при стриминге — 64000.** Поле `Thinking` на Opus 5 не трогаем.
- **Только `https` при обращении к classics.mit.edu.** По `http` сайт молча
  редиректит любой URL на корень и отдаёт главную с кодом 200.
- **Рантайм-зависимости Python — только PyYAML** (в тестах ещё pytest).
  HTML не разбираем, сеть через stdlib `urllib`.
- **Зависимости Go — только Anthropic SDK.** OpenAI-совместимая ветка пишется
  на stdlib `net/http` и `encoding/json`.
- **Модуль Go:** `github.com/alex0x12/aristotle`.
- **Каждая задача заканчивается коммитом.** Сообщения по-русски.

---

## Структура файлов

```
build/                      Python, разовая сборка корпуса
  works.yaml                реестр работ: slug, group, authenticity, источник, URL
  registry.py               загрузка и валидация реестра
  fetch.py                  скачивание с кэшем на диск
  parse_mit.py              разбор .mb.txt в главы
  parse_ws.py               разбор Wikisource (добор дырок)
  emit.py                   запись главы в markdown с frontmatter
  build.py                  оркестровка: реестр -> corpus/ + SOURCES.md
  tests/                    pytest
  fixtures/                 обрезанные реальные .mb.txt для тестов
corpus/                     ~1500 .md, коммитятся
persona/                    01-who … 05-contract
internal/
  config/config.go          профили провайдеров
  corpus/frontmatter.go     разбор frontmatter главы
  corpus/search.go          инструмент search_corpus поверх qmd
  provider/provider.go      интерфейс и нейтральные типы
  provider/anthropic/       официальный SDK
  provider/openaicompat/    OpenAI, Deepseek, OpenRouter
  render/markers.go         потоковый разбор маркеров
  render/verify.go          сверка выдержек
  render/ansi.go            раскраска
  chat/history.go           JSONL
  chat/loop.go              цикл инструмента
  eval/eval.go              прогон метрик
  persona/persona.go        детерминированная сборка системного промпта
cmd/aristotle/main.go       цикл беседы, команды
cmd/aristotle/eval.go       подкоманда прогона метрик
eval/questions.yaml
```

Границы: `render` ничего не знает о провайдерах, `provider` — о персоне и корпусе,
`corpus` — о моделях. `chat` их связывает. Это позволяет тестировать разбор
маркеров и сверку выдержек без единого сетевого вызова.

---

# Фаза A — корпус

## Task 1: Реестр работ

**Файлы:**
- Создать: `build/works.yaml`
- Создать: `build/registry.py`
- Создать: `build/requirements.txt`
- Тест: `build/tests/test_registry.py`

**Интерфейсы:**
- Отдаёт: `registry.load(path: Path) -> dict[str, Work]`, где
  `Work` — frozen dataclass с полями `slug: str`, `title: str`, `group: str`,
  `authenticity: str`, `source: str`, `url: str`, `translator: str | None`.
  Константы `GROUPS`, `AUTHENTICITY`, `SOURCES` — множества допустимых значений.
  Исключение `RegistryError`.

- [ ] **Шаг 1: Написать падающий тест**

```python
# build/tests/test_registry.py
from pathlib import Path
import pytest
from registry import load, Work, RegistryError

FIXTURE = Path(__file__).parent / "fixtures" / "works_ok.yaml"

def test_loads_work():
    works = load(FIXTURE)
    ne = works["nicomachean-ethics"]
    assert ne == Work(
        slug="nicomachean-ethics",
        title="Nicomachean Ethics",
        group="ethics",
        authenticity="genuine",
        source="mit",
        url="https://classics.mit.edu/Aristotle/nicomachaen.mb.txt",
        translator=None,
    )

def test_rejects_unknown_group(tmp_path):
    bad = tmp_path / "bad.yaml"
    bad.write_text(
        "poetics:\n"
        "  title: Poetics\n"
        "  group: poesy\n"
        "  authenticity: genuine\n"
        "  source: mit\n"
        "  url: https://classics.mit.edu/Aristotle/poetics.mb.txt\n",
        encoding="utf-8",
    )
    with pytest.raises(RegistryError, match="group"):
        load(bad)

def test_rejects_missing_field(tmp_path):
    bad = tmp_path / "bad.yaml"
    bad.write_text(
        "poetics:\n"
        "  title: Poetics\n"
        "  group: rhetoric-poetics\n",
        encoding="utf-8",
    )
    with pytest.raises(RegistryError, match="authenticity"):
        load(bad)
```

Создать фикстуру `build/tests/fixtures/works_ok.yaml` с одной записью:

```yaml
nicomachean-ethics:
  title: Nicomachean Ethics
  group: ethics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/nicomachaen.mb.txt
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `cd build && python -m pytest tests/test_registry.py -v`
Ожидается: FAIL, `ModuleNotFoundError: No module named 'registry'`

- [ ] **Шаг 3: Написать минимальную реализацию**

```python
# build/registry.py
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
```

`build/requirements.txt`:

```
PyYAML>=6.0
pytest>=8.0
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `cd build && python -m pytest tests/test_registry.py -v`
Ожидается: PASS, 3 теста

- [ ] **Шаг 5: Заполнить боевой works.yaml**

29 работ MIT — список снят с `browse-Aristotle.html` 31 августа 2026.
URL строится как `https://classics.mit.edu/Aristotle/<base>.mb.txt`.

```yaml
# build/works.yaml
athenian-constitution:
  title: The Athenian Constitution
  group: politics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/athenian_const.mb.txt
categories:
  title: Categories
  group: logic
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/categories.mb.txt
on-dreams:
  title: On Dreams
  group: psychology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/dreams.mb.txt
gait-of-animals:
  title: On the Gait of Animals
  group: biology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/gait_anim.mb.txt
generation-and-corruption:
  title: On Generation and Corruption
  group: physics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/gener_corr.mb.txt
on-the-heavens:
  title: On the Heavens
  group: physics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/heavens.mb.txt
history-of-animals:
  title: The History of Animals
  group: biology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/history_anim.mb.txt
on-interpretation:
  title: On Interpretation
  group: logic
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/interpretation.mb.txt
longevity-and-shortness-of-life:
  title: On Longevity and Shortness of Life
  group: psychology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/longev_short.mb.txt
on-memory:
  title: On Memory and Reminiscence
  group: psychology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/memory.mb.txt
metaphysics:
  title: Metaphysics
  group: metaphysics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/metaphysics.mb.txt
meteorology:
  title: Meteorology
  group: physics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/meteorology.mb.txt
movement-of-animals:
  title: On the Motion of Animals
  group: biology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/motion_animals.mb.txt
nicomachean-ethics:
  title: Nicomachean Ethics
  group: ethics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/nicomachaen.mb.txt
parts-of-animals:
  title: On the Parts of Animals
  group: biology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/parts_animals.mb.txt
physics:
  title: Physics
  group: physics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/physics.mb.txt
poetics:
  title: Poetics
  group: rhetoric-poetics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/poetics.mb.txt
politics:
  title: Politics
  group: politics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/politics.mb.txt
posterior-analytics:
  title: Posterior Analytics
  group: logic
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/posterior.mb.txt
prior-analytics:
  title: Prior Analytics
  group: logic
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/prior.mb.txt
on-prophesying-by-dreams:
  title: On Prophesying by Dreams
  group: psychology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/prophesying.mb.txt
rhetoric:
  title: Rhetoric
  group: rhetoric-poetics
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/rhetoric.mb.txt
on-sense-and-the-sensible:
  title: On Sense and the Sensible
  group: psychology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/sense.mb.txt
on-sleep:
  title: On Sleep and Sleeplessness
  group: psychology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/sleep.mb.txt
sophistical-refutations:
  title: On Sophistical Refutations
  group: logic
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/sophist_refut.mb.txt
on-the-soul:
  title: On the Soul
  group: psychology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/soul.mb.txt
topics:
  title: Topics
  group: logic
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/topics.mb.txt
virtues-and-vices:
  title: Virtues and Vices
  group: ethics
  authenticity: spurious
  source: mit
  url: https://classics.mit.edu/Aristotle/aristot.vir.mb.txt
on-youth-and-old-age:
  title: On Youth and Old Age, On Life and Death, On Breathing
  group: psychology
  authenticity: genuine
  source: mit
  url: https://classics.mit.edu/Aristotle/youth_old.mb.txt
```

- [ ] **Шаг 6: Тест на полноту боевого реестра**

```python
# добавить в build/tests/test_registry.py
WORKS = Path(__file__).parent.parent / "works.yaml"

def test_production_registry_loads():
    works = load(WORKS)
    assert len(works) == 29
    assert works["nicomachean-ethics"].group == "ethics"
    assert works["virtues-and-vices"].authenticity == "spurious"
    assert all(w.url.endswith(".mb.txt") for w in works.values())
    assert all(w.url.startswith("https://") for w in works.values())
```

Выполнить: `cd build && python -m pytest tests/ -v`
Ожидается: PASS, 4 теста

- [ ] **Шаг 7: Коммит**

```bash
git add build/works.yaml build/registry.py build/requirements.txt build/tests/
git commit -m "Реестр работ Аристотеля и его загрузчик"
```

---

## Task 2: Скачивание с кэшем

**Файлы:**
- Создать: `build/fetch.py`
- Тест: `build/tests/test_fetch.py`
- Изменить: `.gitignore` (создать, добавить `build/.cache/`)

**Интерфейсы:**
- Потребляет: ничего.
- Отдаёт: `fetch.fetch(url: str, cache_dir: Path) -> str` — возвращает текст,
  кладя копию в кэш; повторный вызов сети не делает. Исключение `FetchError`.

- [ ] **Шаг 1: Написать падающий тест**

```python
# build/tests/test_fetch.py
from pathlib import Path
import pytest
import fetch


def test_rejects_http(tmp_path):
    with pytest.raises(fetch.FetchError, match="https"):
        fetch.fetch("http://classics.mit.edu/Aristotle/poetics.mb.txt", tmp_path)


def test_caches_and_reuses(tmp_path, monkeypatch):
    calls = []

    def fake_get(url):
        calls.append(url)
        return "Provided by The Internet Classics Archive.\nтело"

    monkeypatch.setattr(fetch, "_get", fake_get)
    url = "https://classics.mit.edu/Aristotle/poetics.mb.txt"

    first = fetch.fetch(url, tmp_path)
    second = fetch.fetch(url, tmp_path)

    assert first == second
    assert calls == [url], "второй вызов не должен ходить в сеть"


def test_detects_homepage_substitution(tmp_path, monkeypatch):
    monkeypatch.setattr(fetch, "_get", lambda url: "<HTML><BODY>главная</BODY></HTML>")
    with pytest.raises(fetch.FetchError, match="подмен"):
        fetch.fetch("https://classics.mit.edu/Aristotle/poetics.mb.txt", tmp_path)
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `cd build && python -m pytest tests/test_fetch.py -v`
Ожидается: FAIL, `ModuleNotFoundError: No module named 'fetch'`

- [ ] **Шаг 3: Написать минимальную реализацию**

```python
# build/fetch.py
import hashlib
import urllib.request
from pathlib import Path

UA = "aristotle-corpus-builder/0.1 (personal research)"
TIMEOUT = 60


class FetchError(Exception):
    pass


def _get(url: str) -> str:
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        return resp.read().decode("utf-8", errors="replace")


def _cache_path(url: str, cache_dir: Path) -> Path:
    name = hashlib.sha256(url.encode()).hexdigest()[:16]
    return Path(cache_dir) / f"{name}.txt"


def fetch(url: str, cache_dir: Path) -> str:
    if not url.startswith("https://"):
        raise FetchError(
            f"только https: по http сайт молча отдаёт главную страницу — {url}")

    cache_dir = Path(cache_dir)
    cache_dir.mkdir(parents=True, exist_ok=True)
    path = _cache_path(url, cache_dir)
    if path.exists():
        return path.read_text(encoding="utf-8")

    body = _get(url)
    if "<HTML" in body.upper()[:400]:
        raise FetchError(
            f"подмена главной страницей вместо текста: {url}")
    path.write_text(body, encoding="utf-8")
    return body
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `cd build && python -m pytest tests/test_fetch.py -v`
Ожидается: PASS, 3 теста

- [ ] **Шаг 5: Создать .gitignore**

```
# .gitignore в корне репозитория
build/.cache/
build/__pycache__/
build/tests/__pycache__/
.pytest_cache/
aristotle
```

- [ ] **Шаг 6: Проверить на живой сети**

Выполнить:
```bash
cd build && python -c "
from pathlib import Path
import fetch
t = fetch.fetch('https://classics.mit.edu/Aristotle/poetics.mb.txt', Path('.cache'))
print(len(t), 'байт'); print(t[:120])
"
```
Ожидается: несколько десятков килобайт и шапка `Provided by The Internet
Classics Archive.` Если приходит `FetchError` про подмену — сайт изменился,
остановиться и разобраться, не обходить проверку.

- [ ] **Шаг 7: Коммит**

```bash
git add build/fetch.py build/tests/test_fetch.py .gitignore
git commit -m "Скачивание текстов с кэшем и защитой от подмены главной"
```

---

## Task 3: Разбор текстов MIT в главы

Формат MIT проверен на живых файлах 31 августа 2026, и он **не такой**, как
предполагала первая редакция плана. Факты, на которых строится парсер:

- `BOOK <римская>` — настоящая граница книги, нумерация глав сбрасывается на 1
  в каждой книге («Физика»: BOOK I → 1..9, BOOK II → 1..9);
- `SECTION <арабская>` — **не граница чего-либо**, а артефакт постраничной
  разбивки сайта: нумерация идёт сквозь секции непрерывно («Афинская полития»:
  SECTION 1 несёт главы 1–22, SECTION 2 продолжает 23–44). Строки выбрасываются,
  работа считается не имеющей деления на книги;
- `Part N` — граница главы в 20 работах из 21. Номер бывает арабским и
  **римским** («Поэтика»: I…XXVI);
- голая цифра — граница главы ровно в одной работе, «Никомаховой этике».

Девять работ переезжают на Wikisource: у семи `.mb.txt` даёт 404, у
«О добродетелях и пороках» текстовой версии нет, а «Политика» приходит в
обрезанной HTML-обёртке гугл-кэша (101 КБ вместо ~380, воспроизводится
стабильно). После переезда на MIT остаётся 20 работ.

**Файлы:**
- Создать: `build/parse_mit.py`
- Тест: `build/tests/test_parse_mit.py`
- Создать: `build/tests/fixtures/mit_books.mb.txt`, `build/tests/fixtures/mit_sections.mb.txt`
- Изменить: `build/works.yaml` (девять работ на Wikisource)
- Изменить: `build/tests/test_registry.py` (счётчики)
- Изменить: `build/fetch.py` (окно проверки подмены)

**Интерфейсы:**
- Потребляет: `registry.Work`, `fetch.fetch`.
- Отдаёт:
  - `parse_mit.Chapter` — frozen dataclass с полями `book: str | None`,
    `number: int`, `text: str`;
  - `parse_mit.roman_to_int(token: str) -> int`;
  - `parse_mit.parse(raw: str) -> tuple[str | None, list[Chapter]]`;
  - исключение `parse_mit.ParseError`.

- [ ] **Шаг 1: Перевести девять работ на Wikisource**

Имена страниц и переводчики проверены через API Wikisource. В `build/works.yaml`
у этих девяти записей заменить `source` и `url` и добавить `translator`:

```yaml
politics:
  title: Politics
  group: politics
  authenticity: genuine
  source: wikisource
  url: https://en.wikisource.org/wiki/Politics_(Ellis)
  translator: William Ellis
gait-of-animals:
  title: On the Gait of Animals
  group: biology
  authenticity: genuine
  source: wikisource
  url: https://en.wikisource.org/wiki/On_the_Progression_of_Animals
  translator: A. S. L. Farquharson
longevity-and-shortness-of-life:
  title: On Longevity and Shortness of Life
  group: psychology
  authenticity: genuine
  source: wikisource
  url: https://en.wikisource.org/wiki/On_Longevity_and_Shortness_of_Life
  translator: George Robert Thompson Ross
movement-of-animals:
  title: On the Motion of Animals
  group: biology
  authenticity: genuine
  source: wikisource
  url: https://en.wikisource.org/wiki/On_the_Movement_of_Animals
  translator: A. S. L. Farquharson
on-dreams:
  title: On Dreams
  group: psychology
  authenticity: genuine
  source: wikisource
  url: https://en.wikisource.org/wiki/On_Dreams_(Aristotle)
  translator: John Isaac Beare
on-memory:
  title: On Memory and Reminiscence
  group: psychology
  authenticity: genuine
  source: wikisource
  url: https://en.wikisource.org/wiki/On_Memory_and_Reminiscence
  translator: John Isaac Beare
on-prophesying-by-dreams:
  title: On Prophesying by Dreams
  group: psychology
  authenticity: genuine
  source: wikisource
  url: https://en.wikisource.org/wiki/On_Prophesying_by_Dreams
  translator: John Isaac Beare
on-sleep:
  title: On Sleep and Sleeplessness
  group: psychology
  authenticity: genuine
  source: wikisource
  url: https://en.wikisource.org/wiki/On_Sleep_and_Sleeplessness
  translator: John Isaac Beare
virtues-and-vices:
  title: Virtues and Vices
  group: ethics
  authenticity: spurious
  source: wikisource
  url: https://en.wikisource.org/wiki/Virtues_and_Vices
  translator: Joseph Solomon
```

Поле `title` остаётся человекочитаемым именем работы; страница Wikisource
выводится парсером из `url`, отдельного поля для неё не заводим.

Обновить тест полноты в `build/tests/test_registry.py`:

```python
def test_production_registry_loads():
    works = load(WORKS)
    assert len(works) == 29
    assert works["nicomachean-ethics"].group == "ethics"
    assert works["virtues-and-vices"].authenticity == "spurious"
    assert all(w.url.startswith("https://") for w in works.values())
    mit = [w for w in works.values() if w.source == "mit"]
    ws = [w for w in works.values() if w.source == "wikisource"]
    assert len(mit) == 20, "девять работ переехали на Wikisource"
    assert len(ws) == 9
    assert all(w.url.endswith(".mb.txt") for w in mit)
    assert all(w.translator for w in ws), "у работ Wikisource переводчик задан явно"
```

Выполнить: `cd build && python -m pytest tests/test_registry.py -v`
Ожидается: PASS.

- [ ] **Шаг 2: Расширить окно проверки подмены в fetch.py**

Проверка `"<HTML" in body.upper()[:400]` не поймала реальный случай: в обёртке
гугл-кэша тег `<html>` стоит на 1285-м байте. Окно расширяется до 4096 символов,
и заодно убирается лишняя аллокация всего тела.

В `build/fetch.py` заменить строку проверки на:

```python
    if "<HTML" in body[:4096].upper():
```

Дописать тест в `build/tests/test_fetch.py`:

```python
def test_detects_late_html_wrapper(tmp_path, monkeypatch):
    # Реальный случай: гугл-кэш ставит <html> примерно на 1285-м байте.
    wrapper = "x" * 1200 + "<html><body><pre>\nProvided by The Internet Classics Archive.\n"
    monkeypatch.setattr(fetch, "_get", lambda url: wrapper)
    with pytest.raises(fetch.FetchError, match="подмен"):
        fetch.fetch("https://classics.mit.edu/Aristotle/politics.mb.txt", tmp_path)
```

Выполнить: `cd build && python -m pytest tests/test_fetch.py -v`
Ожидается: PASS, 6 тестов.

- [ ] **Шаг 3: Собрать две фикстуры из живых файлов**

Нужны обе разновидности разметки: с книгами и с секциями.

```bash
cd build && python - <<'EOF'
from pathlib import Path
import fetch

# 1) книги + голые цифры: Никомахова этика. BOOK II стоит на 769-й строке,
#    поэтому берём 900 строк — иначе граница книги в фикстуру не попадёт.
raw = fetch.fetch('https://classics.mit.edu/Aristotle/nicomachaen.mb.txt', Path('.cache'))
tail = ['', 'THE END', '', '-' * 70, '', 'Copyright statement:', 'ICA']
Path('tests/fixtures/mit_books.mb.txt').write_text(
    '\n'.join(raw.splitlines()[:900] + tail) + '\n', encoding='utf-8')

# 2) секции + Part с римскими номерами: Поэтика целиком, она невелика.
raw = fetch.fetch('https://classics.mit.edu/Aristotle/poetics.mb.txt', Path('.cache'))
Path('tests/fixtures/mit_sections.mb.txt').write_text(raw, encoding='utf-8')
print('фикстуры записаны')
EOF
```

Открыть обе и убедиться глазами: в первой есть `BOOK I`, `BOOK II`, строки
с одинокими `1` и `2`; во второй — `SECTION 1`, `SECTION 2`, `Part I`, `Part XII`.

- [ ] **Шаг 4: Написать падающие тесты**

```python
# build/tests/test_parse_mit.py
from pathlib import Path
import pytest
from parse_mit import Chapter, ParseError, parse, roman_to_int

FIX = Path(__file__).parent / "fixtures"
BOOKS = (FIX / "mit_books.mb.txt").read_text(encoding="utf-8")
SECTIONS = (FIX / "mit_sections.mb.txt").read_text(encoding="utf-8")


def test_roman_to_int():
    assert roman_to_int("I") == 1
    assert roman_to_int("IV") == 4
    assert roman_to_int("XII") == 12
    assert roman_to_int("XXVI") == 26


def test_extracts_translator():
    translator, _ = parse(BOOKS)
    assert translator == "W. D. Ross"


def test_bare_number_style_first_chapter():
    _, chapters = parse(BOOKS)
    first = chapters[0]
    assert first.book == "I"
    assert first.number == 1
    assert first.text.startswith("Every art and every inquiry")


def test_book_resets_chapter_numbering():
    _, chapters = parse(BOOKS)
    by_book: dict[str | None, list[int]] = {}
    for c in chapters:
        by_book.setdefault(c.book, []).append(c.number)
    assert "II" in by_book, "фикстура должна захватывать вторую книгу"
    for nums in by_book.values():
        assert nums == list(range(1, len(nums) + 1))


def test_drops_copyright_tail():
    _, chapters = parse(BOOKS)
    joined = "\n".join(c.text for c in chapters)
    assert "Copyright statement" not in joined
    assert "THE END" not in joined
    assert "Internet Classics Archive" not in joined


def test_reflows_hard_wrapped_lines():
    _, chapters = parse(BOOKS)
    assert "\n" not in chapters[0].text.split("\n\n")[0]


def test_part_style_with_roman_numbers():
    translator, chapters = parse(SECTIONS)
    assert translator == "S. H. Butcher"
    assert [c.number for c in chapters[:3]] == [1, 2, 3]
    assert chapters[-1].number == 26


def test_sections_are_not_books_and_do_not_reset_numbering():
    _, chapters = parse(SECTIONS)
    assert all(c.book is None for c in chapters), "SECTION книгой не является"
    numbers = [c.number for c in chapters]
    assert numbers == list(range(1, len(numbers) + 1)), \
        "нумерация идёт сквозь секции непрерывно"


def test_stray_number_does_not_open_chapter():
    raw = (
        "Some Work\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nBOOK I\n\nPart 1 \n\nfirst chapter text\n\n"
        "he counted them: \n\n7 \n\nstill the first chapter\n\n"
        "Part 2 \n\nsecond chapter text\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert [c.number for c in chapters] == [1, 2]
    assert "still the first chapter" in chapters[0].text


def test_bare_number_ignored_when_work_uses_parts():
    raw = (
        "W\nBy Aristotle\n\nTranslated by Nobody\n\n" + "-" * 70 +
        "\n\nPart 1 \n\nтело\n\n2 \n\nэто не глава, а число в тексте\n\n"
        "Part 2 \n\nвторая\n\nTHE END\n"
    )
    _, chapters = parse(raw)
    assert [c.number for c in chapters] == [1, 2]
    assert "не глава" in chapters[0].text


def test_rejects_text_without_header_rule():
    with pytest.raises(ParseError, match="шапк"):
        parse("Nicomachean Ethics\nBy Aristotle\nno rule line here\n")


def test_rejects_text_without_chapters():
    raw = "W\nBy Aristotle\n\nTranslated by X\n\n" + "-" * 70 + "\n\nтекст\n"
    with pytest.raises(ParseError, match="глав"):
        parse(raw)
```

- [ ] **Шаг 5: Запустить тесты, убедиться что падают**

Выполнить: `cd build && python -m pytest tests/test_parse_mit.py -v`
Ожидается: FAIL, `ModuleNotFoundError: No module named 'parse_mit'`

- [ ] **Шаг 6: Написать реализацию**

```python
# build/parse_mit.py
import re
from dataclasses import dataclass

RULE_RE = re.compile(r"^-{10,}\s*$")
TRANSLATOR_RE = re.compile(r"^Translated by (.+?)\s*$", re.M)
BOOK_RE = re.compile(r"^BOOK\s+([IVXLC]+)\s*$")
SECTION_RE = re.compile(r"^SECTION\s+\d+\s*$")
PART_RE = re.compile(r"^Part\s+(\d+|[IVXLC]+)\s*$")
BARE_RE = re.compile(r"^(\d+)\s*$")
END_RE = re.compile(r"^THE END\s*$")

ROMAN = {"I": 1, "V": 5, "X": 10, "L": 50, "C": 100}


class ParseError(Exception):
    pass


@dataclass(frozen=True)
class Chapter:
    book: str | None
    number: int
    text: str


def roman_to_int(token: str) -> int:
    total = 0
    prev = 0
    for char in reversed(token.upper()):
        value = ROMAN[char]
        if value < prev:
            total -= value
        else:
            total += value
            prev = value
    return total


def _to_int(token: str) -> int:
    return int(token) if token.isdigit() else roman_to_int(token)


def _reflow(lines: list[str]) -> str:
    paragraphs: list[str] = []
    buf: list[str] = []
    for line in lines:
        stripped = line.strip()
        if stripped:
            buf.append(stripped)
        elif buf:
            paragraphs.append(" ".join(buf))
            buf = []
    if buf:
        paragraphs.append(" ".join(buf))
    return "\n\n".join(paragraphs)


def parse(raw: str) -> tuple[str | None, list[Chapter]]:
    lines = raw.splitlines()

    rule_at = next((i for i, l in enumerate(lines) if RULE_RE.match(l)), None)
    if rule_at is None:
        raise ParseError("не найден конец шапки: нет строки из дефисов")

    match = TRANSLATOR_RE.search("\n".join(lines[:rule_at]))
    translator = match.group(1) if match else None

    body = lines[rule_at + 1:]
    end_at = next((i for i, l in enumerate(body) if END_RE.match(l)), None)
    if end_at is not None:
        body = body[:end_at]

    # Стиль разметки определяется один раз на всю работу: если есть хоть одна
    # строка "Part N", голые цифры в тексте главами не считаются.
    uses_parts = any(PART_RE.match(l) for l in body)

    def chapter_number(line: str) -> int | None:
        match = PART_RE.match(line)
        if match:
            return _to_int(match.group(1))
        if uses_parts:
            return None
        match = BARE_RE.match(line)
        return int(match.group(1)) if match else None

    chapters: list[Chapter] = []
    book: str | None = None
    number: int | None = None
    buf: list[str] = []
    last = 0

    def flush() -> None:
        if number is not None:
            chapters.append(Chapter(book=book, number=number, text=_reflow(buf)))

    for line in body:
        # SECTION — артефакт постраничной разбивки сайта, а не деление текста:
        # нумерация глав идёт сквозь секции непрерывно.
        if RULE_RE.match(line) or SECTION_RE.match(line):
            continue
        book_match = BOOK_RE.match(line)
        if book_match:
            flush()
            book, number, buf, last = book_match.group(1), None, [], 0
            continue
        candidate = chapter_number(line)
        if candidate is not None and candidate == last + 1:
            flush()
            number = candidate
            last = candidate
            buf = []
            continue
        if number is not None:
            buf.append(line)

    flush()
    if not chapters:
        raise ParseError("не найдено ни одной главы")
    return translator, chapters
```

- [ ] **Шаг 7: Запустить тесты, убедиться что проходят**

Выполнить: `cd build && python -m pytest tests/ -v`
Ожидается: PASS, все тесты пакета.

- [ ] **Шаг 8: Живой прогон по двадцати работам MIT**

```bash
cd build && python - <<'EOF'
from pathlib import Path
import fetch, parse_mit, registry
works = {k: w for k, w in registry.load(Path('works.yaml')).items() if w.source == 'mit'}
total = 0
for slug, w in sorted(works.items()):
    raw = fetch.fetch(w.url, Path('.cache'))
    tr, ch = parse_mit.parse(raw)
    books = sorted({c.book for c in ch}, key=lambda b: (b is None, b or ''))
    total += len(ch)
    print(f'{slug:34s} глав={len(ch):4d} книг={len(books):2d} пер.={tr}')
print('работ:', len(works), 'всего глав:', total)
EOF
```

Ожидается: 20 строк без исключений, у каждой работы переводчик найден,
суммарно порядка 900–1300 глав.

Признаки, по которым останавливаться и разбираться, а не подгонять регулярку:
ноль глав у какой-либо работы, отсутствие переводчика, число глав меньше числа
книг. Если расхождение окажется структурным — сообщить со статусом BLOCKED,
приложив название работы и кусок её разметки.

- [ ] **Шаг 9: Коммит**

```bash
git add build/parse_mit.py build/tests/test_parse_mit.py build/tests/fixtures/ \
        build/works.yaml build/tests/test_registry.py build/fetch.py \
        build/tests/test_fetch.py
git commit -m "Разбор текстов MIT по фактической разметке и перевод девяти работ на Wikisource"
```

---

## Task 4: Запись корпуса

**Файлы:**
- Создать: `build/emit.py`
- Создать: `build/build.py`
- Тест: `build/tests/test_emit.py`

**Интерфейсы:**
- Потребляет: `registry.Work` (Task 1), `parse_mit.Chapter` (Task 3).
- Отдаёт:
  - `emit.chapter_path(work: Work, chapter: Chapter) -> Path` — относительный
    путь вида `nicomachean-ethics/i/07.md` или `poetics/06.md`;
  - `emit.render(work: Work, chapter: Chapter, translator: str | None) -> str` —
    полный текст файла с frontmatter;
  - `emit.write(root: Path, work, chapter, translator) -> Path`.

- [ ] **Шаг 1: Написать падающий тест**

```python
# build/tests/test_emit.py
from pathlib import Path
import emit
from registry import Work
from parse_mit import Chapter

WORK = Work(
    slug="nicomachean-ethics",
    title="Nicomachean Ethics",
    group="ethics",
    authenticity="genuine",
    source="mit",
    url="https://classics.mit.edu/Aristotle/nicomachaen.mb.txt",
    translator=None,
)
CH = Chapter(book="I", number=7, text="The good of man is an activity of soul.")


def test_path_with_book():
    assert emit.chapter_path(WORK, CH) == Path("nicomachean-ethics/i/07.md")


def test_path_without_book():
    ch = Chapter(book=None, number=6, text="tragedy is an imitation")
    work = Work("poetics", "Poetics", "rhetoric-poetics", "genuine", "mit", "u")
    assert emit.chapter_path(work, ch) == Path("poetics/06.md")


def test_render_frontmatter():
    out = emit.render(WORK, CH, "W. D. Ross")
    assert out.startswith("---\n")
    assert 'work: "Nicomachean Ethics"' in out
    assert "work_slug: nicomachean-ethics" in out
    assert 'book: "I"' in out
    assert "chapter: 7" in out
    assert "group: ethics" in out
    assert "authenticity: genuine" in out
    assert 'translator: "W. D. Ross"' in out
    assert 'cite: "Nicomachean Ethics I.7"' in out
    assert out.rstrip().endswith("The good of man is an activity of soul.")


def test_render_cite_without_book():
    ch = Chapter(book=None, number=6, text="tragedy is an imitation")
    work = Work("poetics", "Poetics", "rhetoric-poetics", "genuine", "mit", "u")
    out = emit.render(work, ch, "S. H. Butcher")
    assert 'cite: "Poetics 6"' in out
    assert "book: null" in out


def test_write_creates_file(tmp_path):
    path = emit.write(tmp_path, WORK, CH, "W. D. Ross")
    assert path.exists()
    assert path.relative_to(tmp_path) == Path("nicomachean-ethics/i/07.md")
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `cd build && python -m pytest tests/test_emit.py -v`
Ожидается: FAIL, `ModuleNotFoundError: No module named 'emit'`

- [ ] **Шаг 3: Написать минимальную реализацию**

```python
# build/emit.py
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
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `cd build && python -m pytest tests/test_emit.py -v`
Ожидается: PASS, 5 тестов

- [ ] **Шаг 5: Написать оркестровку**

```python
# build/build.py
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
```

- [ ] **Шаг 6: Собрать корпус и проверить результат**

Выполнить:
```bash
cd build && python build.py
find ../corpus -name '*.md' | wc -l
head -20 ../corpus/nicomachean-ethics/i/07.md
```
Ожидается: собраны 20 работ MIT; тринадцать работ Wikisource на этом шаге
пропускаются со строкой «нет парсера для источника wikisource» — их берёт
на себя задача 5. Число файлов совпадает с «всего глав»; в файле НЭ I.7 фронтматтер
с `cite: "Nicomachean Ethics I.7"` и текст про благо человека.

- [ ] **Шаг 7: Проверить, что ни в один файл не утёк подвал**

Выполнить: `grep -rl "Internet Classics Archive" ../corpus/ | head`
Ожидается: пустой вывод. Если что-то нашлось — вернуться к Task 3, а не
подчищать корпус вручную.

- [ ] **Шаг 8: Коммит**

```bash
git add build/emit.py build/build.py build/tests/test_emit.py corpus/
git commit -m "Запись корпуса в markdown по главам и сборка двадцати работ MIT"
```

---

## Task 5: Разбор Wikisource

Wikisource несёт **9 работ**: девять переехали в задаче 3 (семь с 404 на MIT,
«О добродетелях и пороках» без текстовой версии, «Политика» в обрезанной
гугл-обёртке) и одна добавляется здесь — «О возникновении животных», которой
на MIT нет, а сноска `[факт]` из спеки 5.3 на неё опирается.

Итог по корпусу: **29 работ, все подлинные**, 20 с MIT и 9 с Wikisource.

**Четыре работы выброшены сознательно** — их текст на Wikisource недостижим,
проверено 31 августа 2026:

- «Евдемова этика» — трансклюзия Proofread-Page. И корневая страница, и все
  подстраницы (`Book 1, 2, 3, 7`) в сыром вики-тексте содержат только
  `<pages index="Works of Aristotle v9 (ed. Ross).djvu" from=425 to=437 />`;
  263 байта шаблона, текста ноль;
- «О растениях» — то же самое, 663 байта;
- «Проблемы» — страницы нет: имя `Problems` оказалось редиректом на
  стихотворение Эмили Дикинсон, поиск другого варианта ничего не дал;
- «О добродетелях и пороках» — трансклюзия из того же скана, 558 байт.

Достать их можно было бы только через отрисованный HTML, то есть заведя в
конвейер разбор HTML, от которого мы отказались. Проверено, что **никакой другой
спурии на Wikisource нет вовсе** — ни «О мире», ни «Риторики к Александру»,
ни «Механики», ни «Экономики», ни «О цветах», ни «О чудесных слухах».

Поэтому корпус целиком состоит из подлинных сочинений, а способность отвечать
«этого я не писал» переезжает в персону: список ложно приписываемых работ живёт
в `04-doctrine.md` (задача 14). Текст «Экономики» для ответа «я её не писал»
не нужен — нужен список.

Строение Wikisource проверено через API и оно неоднородно:

- **подстраницы перечисляются запросом**, а не строятся по шаблону:
  `action=query&list=allpages&apprefix=<Страница>/`. У «Политики (Эллис)» это
  `Book 1`…`Book 8`, у «О возникновении животных» — `Book I`…`Book V` (римские);
- **среди подстраниц есть не-аристотелевские**: `Politics (Ellis)/Introduction`.
  Берутся только книгоподобные;
- **у многих работ подстраниц нет вовсе** — весь текст на одной странице;
- сырой вики-текст: `https://en.wikisource.org/w/index.php?title=<Страница>&action=raw`;
- **уровень заголовка главы разный у разных работ.** У «Политики (Эллис)»
  глава — заголовок **третьего** уровня (`=== CHAPTER I ===`), а второй уровень
  занят книгой (`== BOOK I ==`). У «О движении животных» глава — второй уровень
  (`== 1 ==`). У прочих — `== Part N ==`. Поэтому уровень определяется
  по данным: **глава размечена тем уровнем заголовков, которого на странице
  больше всего**;
- номер главы бывает арабским и римским; работа без заголовков — одна глава;
- абзацы разделены пустой строкой и **не** переносятся жёстко.

Три короткие работы идут без деления на главы и станут одной главой каждая:
`On the Progression of Animals`, `On Dreams (Aristotle)`, `Virtues and Vices`.

**Файлы:**
- Создать: `build/parse_ws.py`
- Тест: `build/tests/test_parse_ws.py`
- Изменить: `build/works.yaml` (одна новая работа)
- Изменить: `build/tests/test_registry.py` (счётчики)
- Изменить: `build/build.py` (ветвление по источнику)

**Интерфейсы:**
- Потребляет: `parse_mit.Chapter`, `parse_mit.ParseError`, `parse_mit.roman_to_int`.
- Отдаёт:
  - `parse_ws.page_title(url: str) -> str`;
  - `parse_ws.book_subpages(title: str, get) -> list[tuple[str, str]]`;
  - `parse_ws.chapter_level(raw: str) -> int | None` — самый частый уровень
    заголовков, либо `None`, если заголовков нет;
  - `parse_ws.clean_wikitext(text: str) -> str`;
  - `parse_ws.check_usable(raw: str, page: str) -> None` — поднимает `ParseError`
    на трансклюзии и редиректе;
  - `parse_ws.parse_page(raw: str, book: str | None) -> tuple[str | None, list[Chapter]]`;
  - `parse_ws.parse_work(work, get) -> tuple[str | None, list[Chapter]]`.

- [ ] **Шаг 1: Написать падающие тесты**

```python
# build/tests/test_parse_ws.py
import json
import pytest
from parse_mit import ParseError
from parse_ws import (book_subpages, chapter_level, check_usable,
                      clean_wikitext, page_title, parse_page)

APOS = "'" * 3

PART_PAGE = f"""{{{{header
 | translator = Arthur Platt
 | section    = {APOS}Book I{APOS}
}}}}

== Part 1 ==

WE have now discussed the other parts of animals.

There are four causes underlying everything.

== Part 2 ==

Now the female is as it were a deformed male.
"""

# «Политика (Эллис)»: книга — второй уровень, глава — третий.
NESTED_PAGE = """{{header
 | translator = William Ellis
 | section  = Book 1
}}

== BOOK I ==

=== CHAPTER I ===

Every state is a community of some kind.

=== CHAPTER II ===

He who thus considers things in their first growth.

=== CHAPTER III ===

Seeing then that the state is made up of households.
"""

BARE_HEADINGS = """{{header
 | translator = A. S. L. Farquharson
}}

== 1 ==

первая глава

== 2 ==

вторая глава
"""

NO_HEADINGS = """{{header
 | translator = Joseph Solomon
}}

Весь текст одним куском, заголовков нет.

Второй абзац.
"""


def test_page_title_from_url():
    assert page_title("https://en.wikisource.org/wiki/Politics_(Ellis)") == "Politics (Ellis)"
    assert page_title("https://en.wikisource.org/wiki/On_Dreams_(Aristotle)") == "On Dreams (Aristotle)"


def test_chapter_level_picks_most_frequent():
    assert chapter_level(PART_PAGE) == 2
    assert chapter_level(NESTED_PAGE) == 3, "у Политики глав больше, чем книг"
    assert chapter_level(BARE_HEADINGS) == 2
    assert chapter_level(NO_HEADINGS) is None


def test_parse_page_part_style():
    translator, chapters = parse_page(PART_PAGE, "I")
    assert translator == "Arthur Platt"
    assert [c.number for c in chapters] == [1, 2]
    assert all(c.book == "I" for c in chapters)
    assert chapters[0].text.startswith("WE have now discussed")
    assert "\n\n" in chapters[0].text


def test_nested_headings_book_line_is_not_a_chapter():
    _, chapters = parse_page(NESTED_PAGE, "I")
    assert [c.number for c in chapters] == [1, 2, 3], \
        "строка BOOK I главой быть не должна"
    assert chapters[0].text == "Every state is a community of some kind."
    assert "BOOK" not in "\n".join(c.text for c in chapters)


def test_bare_numeric_headings_are_chapters():
    _, chapters = parse_page(BARE_HEADINGS, None)
    assert [c.number for c in chapters] == [1, 2]
    assert chapters[0].text == "первая глава"


def test_page_without_headings_is_one_chapter():
    translator, chapters = parse_page(NO_HEADINGS, None)
    assert translator == "Joseph Solomon"
    assert len(chapters) == 1
    assert chapters[0].number == 1
    assert "Второй абзац." in chapters[0].text


def test_clean_strips_nested_templates():
    src = "{{translation license|original={{PD-old}}|translation={{PD-1923}}}} текст"
    assert clean_wikitext(src).strip() == "текст"


def test_clean_strips_refs_and_categories():
    src = ("текст<ref>сноска с [[ссылкой]]</ref> дальше\n"
           "[[Category:Aristotle]]\n[[fr:Politique]]\n")
    out = clean_wikitext(src)
    assert "сноска" not in out
    assert "Category" not in out
    assert "fr:" not in out
    assert "текст дальше" in out


def test_clean_keeps_link_text():
    assert clean_wikitext("см. [[Physics|Физику]] и [[Topics]]") == "см. Физику и Topics"


def test_check_usable_rejects_transclusion():
    raw = '{{header}}\n<pages index="Works of Aristotle v9.djvu" from=425 to=437 />'
    with pytest.raises(ParseError, match="трансклюзи"):
        check_usable(raw, "Eudemian Ethics")


def test_check_usable_rejects_redirect():
    with pytest.raises(ParseError, match="редирект"):
        check_usable("#redirect [[Bring me the sunset in a cup]]", "Problems")


def test_check_usable_accepts_normal_page():
    check_usable(PART_PAGE, "On the Generation of Animals/Book I")


def test_book_subpages_filters_non_books_and_sorts():
    def fake_get(url):
        return json.dumps({"query": {"allpages": [
            {"title": "Politics (Ellis)/Book 10"},
            {"title": "Politics (Ellis)/Book 2"},
            {"title": "Politics (Ellis)/Introduction"},
            {"title": "Politics (Ellis)/Book 1"},
        ]}})

    pages = book_subpages("Politics (Ellis)", fake_get)
    assert [name for name, _ in pages] == [
        "Politics (Ellis)/Book 1",
        "Politics (Ellis)/Book 2",
        "Politics (Ellis)/Book 10",
    ]
    assert [book for _, book in pages] == ["I", "II", "X"]


def test_book_subpages_accepts_roman():
    def fake_get(url):
        return json.dumps({"query": {"allpages": [
            {"title": "On the Generation of Animals/Book II"},
            {"title": "On the Generation of Animals/Book I"},
        ]}})

    assert [b for _, b in book_subpages("On the Generation of Animals", fake_get)] == ["I", "II"]


def test_book_subpages_empty_when_no_subpages():
    def fake_get(url):
        return json.dumps({"query": {"allpages": []}})

    assert book_subpages("On Dreams (Aristotle)", fake_get) == []
```

- [ ] **Шаг 2: Запустить тесты, убедиться что падают**

Выполнить: `cd build && python -m pytest tests/test_parse_ws.py -v`
Ожидается: FAIL, `ModuleNotFoundError: No module named 'parse_ws'`

- [ ] **Шаг 3: Написать реализацию**

```python
# build/parse_ws.py
import json
import re
import time
from urllib.parse import quote, unquote

from parse_mit import Chapter, ParseError, roman_to_int

API = "https://en.wikisource.org/w/api.php"
RAW = "https://en.wikisource.org/w/index.php"
DELAY = 1.0  # API отвечает 429 на частые запросы

TRANSLATOR_RE = re.compile(r"^\s*\|\s*translator\s*=\s*(.+?)\s*$", re.M)
HEADING_RE = re.compile(r"^(=+)\s*(.+?)\s*=+\s*$")
NUMBER_RE = re.compile(r"(\d+|[IVXLC]+)\s*$")
BOOK_SUB_RE = re.compile(r"/Book\s+(\d+|[IVXLC]+)\s*$", re.I)
REF_RE = re.compile(r"<ref[^>]*>.*?</ref>|<ref[^>]*/>", re.S | re.I)
TAG_RE = re.compile(r"</?[a-z][^>]*>", re.I)
DROP_LINK_RE = re.compile(r"\[\[(?:Category|File|Image|[a-z]{2,3}):[^\]]*\]\]", re.I)
LINK_RE = re.compile(r"\[\[(?:[^\]|]*\|)?([^\]]*)\]\]")
BOLD_RE = re.compile(r"'{2,}")
TRANSCLUSION_RE = re.compile(r"<pages\b", re.I)
REDIRECT_RE = re.compile(r"^\s*#redirect", re.I)

ROMAN_OUT = ["", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X",
             "XI", "XII", "XIII", "XIV", "XV"]


def page_title(url: str) -> str:
    return unquote(url.rsplit("/", 1)[-1]).replace("_", " ")


def _to_int(token: str) -> int:
    return int(token) if token.isdigit() else roman_to_int(token)


def check_usable(raw: str, page: str) -> None:
    """Отвергает страницы, из которых текст не достать.

    Обе формы отдают правдоподобный ответ с кодом 200, и без этой проверки
    в корпус попадут главы, состоящие из шаблона вместо слов Аристотеля.
    """
    if REDIRECT_RE.match(raw):
        raise ParseError(f"{page}: страница — редирект, а не текст")
    if TRANSCLUSION_RE.search(raw):
        raise ParseError(
            f"{page}: текст лежит трансклюзией Proofread-Page, "
            f"в сыром вики-тексте его нет")


def _strip_templates(text: str) -> str:
    """Снимает шаблоны, в том числе вложенные, изнутри наружу."""
    pattern = re.compile(r"\{\{[^{}]*\}\}")
    while True:
        text, count = pattern.subn("", text)
        if not count:
            return text


def clean_wikitext(text: str) -> str:
    text = REF_RE.sub("", text)
    text = _strip_templates(text)
    text = DROP_LINK_RE.sub("", text)
    text = LINK_RE.sub(r"\1", text)
    text = TAG_RE.sub("", text)
    return BOLD_RE.sub("", text)


def chapter_level(raw: str) -> int | None:
    """Уровень заголовка, которым размечены главы.

    У разных работ он разный: у «Политики» глава — третий уровень, а второй
    занят книгой. Берём тот уровень, которого на странице больше всего.
    """
    counts: dict[int, int] = {}
    for line in raw.splitlines():
        match = HEADING_RE.match(line)
        if match:
            level = len(match.group(1))
            counts[level] = counts.get(level, 0) + 1
    if not counts:
        return None
    return max(counts, key=lambda level: (counts[level], -level))


def book_subpages(title: str, get) -> list[tuple[str, str]]:
    url = (f"{API}?action=query&list=allpages"
           f"&apprefix={quote(title + '/')}&aplimit=200&format=json")
    data = json.loads(get(url))
    found: list[tuple[int, str]] = []
    for page in data.get("query", {}).get("allpages", []):
        match = BOOK_SUB_RE.search(page["title"])
        if match:
            found.append((_to_int(match.group(1)), page["title"]))
    found.sort()
    return [(name, ROMAN_OUT[number]) for number, name in found]


def _reflow(lines: list[str]) -> str:
    body = clean_wikitext("\n".join(lines)).strip()
    return re.sub(r"\n{3,}", "\n\n", body)


def parse_page(raw: str, book: str | None) -> tuple[str | None, list[Chapter]]:
    match = TRANSLATOR_RE.search(raw)
    translator = match.group(1) if match else None

    level = chapter_level(raw)
    lines = raw.splitlines()
    chapters: list[Chapter] = []
    number: int | None = None
    buf: list[str] = []

    def flush() -> None:
        if number is not None:
            chapters.append(Chapter(book=book, number=number, text=_reflow(buf)))

    for line in lines:
        heading = HEADING_RE.match(line)
        if heading:
            if len(heading.group(1)) != level:
                continue  # заголовок другого уровня — не граница главы
            flush()
            found = NUMBER_RE.search(heading.group(2))
            number = _to_int(found.group(1)) if found else len(chapters) + 1
            buf = []
            continue
        if number is not None:
            buf.append(line)
    flush()

    if not chapters:
        body = _reflow(lines)
        if not body:
            raise ParseError("страница пуста: ни заголовков, ни текста")
        chapters = [Chapter(book=book, number=1, text=body)]

    return translator, chapters


def parse_work(work, get) -> tuple[str | None, list[Chapter]]:
    """Совместим по смыслу с parse_mit.parse, но ходит в сеть через get."""
    title = page_title(work.url)
    subpages = book_subpages(title, get)
    targets = subpages or [(title, None)]

    translator: str | None = None
    chapters: list[Chapter] = []
    for index, (name, book) in enumerate(targets):
        if index:
            time.sleep(DELAY)
        raw = get(f"{RAW}?title={quote(name)}&action=raw")
        check_usable(raw, name)
        found, page_chapters = parse_page(raw, book)
        translator = translator or found
        chapters.extend(page_chapters)

    if not chapters:
        raise ParseError(f"{work.slug}: не найдено ни одной главы")
    return translator, chapters
```

- [ ] **Шаг 4: Запустить тесты, убедиться что проходят**

Выполнить: `cd build && python -m pytest tests/test_parse_ws.py -v`
Ожидается: PASS, 15 тестов.

- [ ] **Шаг 5: Поправить реестр**

Удалить запись `virtues-and-vices` целиком: её текст недостижим (трансклюзия),
а другой спурии нет ни на одном источнике. Дописать работу, которой нет на MIT:

```yaml
generation-of-animals:
  title: On the Generation of Animals
  group: biology
  authenticity: genuine
  source: wikisource
  url: https://en.wikisource.org/wiki/On_the_Generation_of_Animals
  translator: Arthur Platt
```

Обновить счётчики в `build/tests/test_registry.py`:

```python
    assert len(works) == 29
    mit = [w for w in works.values() if w.source == "mit"]
    ws = [w for w in works.values() if w.source == "wikisource"]
    assert len(mit) == 20
    assert len(ws) == 9
    assert all(w.authenticity == "genuine" for w in works.values()), \
        "спурия недостижима ни из одного источника, см. преамбулу задачи"
    assert all(w.url.endswith(".mb.txt") for w in mit)
    assert all(w.translator for w in ws), "у работ Wikisource переводчик задан явно"
```

- [ ] **Шаг 6: Подключить парсер в оркестровку**

В `build/build.py` завести ветвление по источнику:

```python
import parse_ws

def _chapters(work, get):
    if work.source == "mit":
        return parse_mit.parse(fetch.fetch(work.url, CACHE))
    if work.source == "wikisource":
        return parse_ws.parse_work(work, get)
    raise ValueError(f"неизвестный источник {work.source}")
```

В `main()` завести `get = lambda url: fetch.fetch(url, CACHE)` и звать
`translator, chapters = _chapters(work, get)`. Прежнюю строку с `PARSERS.get(...)`
и её проверку удалить.

- [ ] **Шаг 7: Пересобрать корпус целиком**

Пересборка обязательна: корпус на диске несёт 137 глав «Метафизики»
с испорченным началом — дефект уже исправлен в парсере, но файлы не
обновлялись.

```bash
cd build && python build.py --force
find ../corpus -name '*.md' | wc -l
ls ../corpus/politics/ ../corpus/generation-of-animals/
```

Ожидается: у «Политики» каталоги `i`…`viii`, у «О возникновении животных» —
`i`…`v`. Если у «Политики» книг меньше восьми — остановиться и сказать.

- [ ] **Шаг 8: Три проверки корпуса**

```bash
cd /Development/aristotle
grep -rlE '^" *"' corpus/ | wc -l                    # ждём 0
grep -rnE '^[[:punct:]]$' corpus/ | wc -l            # ждём 0
grep -rn 'pages index\|#redirect\|Category:' corpus/ | wc -l   # ждём 0
grep -rl "Internet Classics Archive" corpus/ | wc -l # ждём 0
```

Все четыре должны дать ноль. Любое ненулевое — дефект разбора, чинить
в парсере, а не вычищать корпус руками.

- [ ] **Шаг 9: Коммит**

```bash
git add build/parse_ws.py build/tests/test_parse_ws.py build/works.yaml \
        build/build.py build/tests/test_registry.py corpus/
git commit -m "Разбор Wikisource и сборка корпуса из двадцати девяти работ"
```

---

## Task 6: SOURCES.md и коллекция qmd

**Файлы:**
- Создать: `build/sources.py`
- Изменить: `build/build.py` (вызов генерации)
- Тест: `build/tests/test_sources.py`
- Создать: `SOURCES.md` (генерируется)
- Создать: `README.md`, `LICENSE`

**Интерфейсы:**
- Потребляет: `registry.Work`, собранный корпус.
- Отдаёт: `sources.render(works: dict[str, Work], counts: dict[str, int]) -> str`.

- [ ] **Шаг 1: Написать падающий тест**

```python
# build/tests/test_sources.py
from registry import Work
import sources

WORKS = {
    "nicomachean-ethics": Work(
        "nicomachean-ethics", "Nicomachean Ethics", "ethics", "genuine",
        "mit", "https://classics.mit.edu/Aristotle/nicomachaen.mb.txt",
        "W. D. Ross"),
    "problems": Work(
        "problems", "Problems", "biology", "spurious",
        "wikisource", "https://en.wikisource.org/wiki/Problems", "E. S. Forster"),
}


def test_lists_every_work():
    out = sources.render(WORKS, {"nicomachean-ethics": 10, "problems": 38})
    assert "Nicomachean Ethics" in out
    assert "W. D. Ross" in out
    assert "Problems" in out


def test_marks_spurious():
    out = sources.render(WORKS, {"nicomachean-ethics": 10, "problems": 38})
    problems_row = [l for l in out.splitlines() if "Problems" in l][0]
    assert "spurious" in problems_row


def test_states_public_domain_reasoning():
    out = sources.render(WORKS, {})
    assert "общественном достоянии" in out
    assert "вёрстк" in out, "должно быть сказано, что вёрстка не воспроизводится"
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `cd build && python -m pytest tests/test_sources.py -v`
Ожидается: FAIL, `ModuleNotFoundError: No module named 'sources'`

- [ ] **Шаг 3: Написать минимальную реализацию**

```python
# build/sources.py
from registry import Work

PREAMBLE = """# Источники корпуса

Тексты в `corpus/` — английские переводы Аристотеля, находящиеся
**в общественном достоянии** по возрасту (издания 1908–1931 годов и старше).

## О правах

Internet Classics Archive помещает на своих страницах уведомление о том, что
их **веб-представление** защищено авторским правом. Конвейер сборки использует
текстовые версии (`.mb.txt`) и снимает всю разметку: в `corpus/` попадает только
текст перевода. Авторское право не возникает от простого воспроизведения
произведения, находящегося в общественном достоянии, — вёрстка, навигация
и комментарии ICA здесь не воспроизводятся.

Тексты с Wikisource взяты как сырой wikitext через открытый API и очищены
от разметки; сами переводы также в общественном достоянии.

Лицензия `LICENSE` (MIT) покрывает **код** этого репозитория. Тексты в `corpus/`
ею не охватываются: они в общественном достоянии.

## Состав

"""

HEADER = ("| Работа | Переводчик | Подлинность | Глав | Источник |\n"
          "|---|---|---|---|---|\n")


def render(works: dict[str, Work], counts: dict[str, int]) -> str:
    rows = []
    for slug, w in sorted(works.items(), key=lambda kv: kv[1].title):
        rows.append(
            f"| [{w.title}]({w.url}) | {w.translator or '—'} "
            f"| {w.authenticity} | {counts.get(slug, 0)} | {w.source} |")
    return PREAMBLE + HEADER + "\n".join(rows) + "\n"
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `cd build && python -m pytest tests/test_sources.py -v`
Ожидается: PASS, 3 теста

- [ ] **Шаг 5: Подключить генерацию и записать переводчика в реестр**

В `build.py`: накапливать `counts[slug] = len(chapters)` и `found[slug] = translator`,
а в конце — записать `SOURCES.md`, подставив найденных переводчиков:

```python
import dataclasses
import sources

# в конце main(), после цикла:
enriched = {
    slug: dataclasses.replace(w, translator=w.translator or found.get(slug))
    for slug, w in works.items()
}
(ROOT / "SOURCES.md").write_text(
    sources.render(enriched, counts), encoding="utf-8")
print("SOURCES.md записан")
```

- [ ] **Шаг 6: Пересобрать и прочитать SOURCES.md глазами**

Выполнить: `cd build && python build.py --force && head -40 ../SOURCES.md`
Ожидается: таблица из 29 строк, у каждой указан переводчик. Прочерк в колонке
переводчика означает, что парсер его не нашёл — вписать вручную
в `works.yaml` полем `translator`, это законное применение реестра.

- [ ] **Шаг 7: Написать README.md и LICENSE**

`LICENSE` — стандартный текст MIT, правообладатель по данным git: `alex0x12`.

`README.md` — что это, как собрать корпус, как завести коллекцию qmd,
как запустить агента, откуда берутся ключи. Раздел про права — ссылкой
на `SOURCES.md`, не дублировать.

- [ ] **Шаг 8: Завести коллекцию qmd и построить эмбеддинги**

Выполнить:
```bash
qmd collection add aristotle /Development/aristotle/corpus --pattern '**/*.md'
qmd context add qmd://aristotle/ "Полный корпус Аристотеля по-английски, один файл на главу, с полями authenticity и group во frontmatter"
qmd embed -c aristotle
qmd status
```
Ожидается: в `qmd status` коллекция `aristotle` с числом файлов, равным числу
`.md` в `corpus/`. Шаг `embed` — минуты; это разовая цена.

- [ ] **Шаг 9: Проверить поиск вручную**

Выполнить: `qmd query "what is happiness the good for man" -c aristotle --json -n 5`
Ожидается: среди пяти попаданий есть `nicomachean-ethics/i/07.md`.
Если нет — не подгонять, а записать факт: это вход в Task 16, где
recall меряется системно.

- [ ] **Шаг 10: Коммит**

```bash
git add build/sources.py build/tests/test_sources.py build/build.py \
        SOURCES.md README.md LICENSE
git commit -m "SOURCES.md, README, лицензия и коллекция qmd"
```

---

# Фаза B — агент

## Task 7: Скелет Go и профили провайдеров

**Файлы:**
- Создать: `go.mod`
- Создать: `internal/config/config.go`
- Тест: `internal/config/config_test.go`

**Интерфейсы:**
- Отдаёт:
  - `config.Profile{Name, Kind, BaseURL, Model, KeyEnv string; Reasoning bool}`,
    где `Kind` — `"anthropic"` либо `"openai"`;
  - `config.Config{Default string; Profiles map[string]Profile}`;
  - `config.Builtin() Config` — встроенные профили;
  - `config.Load(path string) (Config, error)` — читает JSON поверх встроенных,
    отсутствие файла ошибкой не является;
  - `(Config) Resolve(name string) (Profile, error)` — пустое имя даёт `Default`;
  - `(Profile) APIKey() (string, error)` — читает переменную `KeyEnv`.

- [ ] **Шаг 1: Завести модуль**

Выполнить:
```bash
cd /Development/aristotle && go mod init github.com/alex0x12/aristotle
go get github.com/anthropics/anthropic-sdk-go
```

- [ ] **Шаг 2: Написать падающий тест**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinHasFourProfiles(t *testing.T) {
	c := Builtin()
	for _, name := range []string{"claude", "deepseek", "openai", "openrouter"} {
		if _, ok := c.Profiles[name]; !ok {
			t.Fatalf("нет встроенного профиля %q", name)
		}
	}
	if c.Default != "claude" {
		t.Fatalf("Default = %q, ожидался claude", c.Default)
	}
}

func TestClaudeProfileDefaults(t *testing.T) {
	p := Builtin().Profiles["claude"]
	if p.Kind != "anthropic" {
		t.Fatalf("Kind = %q", p.Kind)
	}
	if p.Model != "claude-opus-5" {
		t.Fatalf("Model = %q, ожидался claude-opus-5", p.Model)
	}
}

func TestResolveEmptyGivesDefault(t *testing.T) {
	p, err := Builtin().Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "claude" {
		t.Fatalf("Name = %q", p.Name)
	}
}

func TestResolveUnknownFails(t *testing.T) {
	if _, err := Builtin().Resolve("gpt-9"); err == nil {
		t.Fatal("ожидалась ошибка для неизвестного профиля")
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "нет-такого.json"))
	if err != nil {
		t.Fatalf("отсутствие файла не должно быть ошибкой: %v", err)
	}
	if len(c.Profiles) == 0 {
		t.Fatal("встроенные профили должны остаться")
	}
}

func TestLoadOverridesModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"default":"deepseek","profiles":{"claude":{"model":"claude-sonnet-5"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Default != "deepseek" {
		t.Fatalf("Default = %q", c.Default)
	}
	if got := c.Profiles["claude"].Model; got != "claude-sonnet-5" {
		t.Fatalf("Model = %q", got)
	}
	if got := c.Profiles["claude"].Kind; got != "anthropic" {
		t.Fatalf("Kind затёрт: %q", got)
	}
}

func TestAPIKeyMissing(t *testing.T) {
	p := Profile{Name: "x", KeyEnv: "ARISTOTLE_TEST_MISSING_KEY"}
	os.Unsetenv(p.KeyEnv)
	if _, err := p.APIKey(); err == nil {
		t.Fatal("ожидалась ошибка при отсутствии переменной")
	}
}
```

- [ ] **Шаг 3: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/config/ -v`
Ожидается: FAIL — пакет не собирается, `undefined: Builtin`

- [ ] **Шаг 4: Написать минимальную реализацию**

```go
// internal/config/config.go
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Profile описывает один способ достучаться до модели.
type Profile struct {
	Name      string `json:"-"`
	Kind      string `json:"kind,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
	KeyEnv    string `json:"key_env,omitempty"`
	Reasoning bool   `json:"reasoning,omitempty"`
}

type Config struct {
	Default  string             `json:"default,omitempty"`
	Profiles map[string]Profile `json:"profiles,omitempty"`
}

func Builtin() Config {
	return Config{
		Default: "claude",
		Profiles: map[string]Profile{
			"claude": {
				Kind:   "anthropic",
				Model:  "claude-opus-5",
				KeyEnv: "ANTHROPIC_API_KEY",
			},
			"deepseek": {
				Kind:    "openai",
				BaseURL: "https://api.deepseek.com/v1",
				Model:   "deepseek-v4-pro",
				KeyEnv:  "DEEPSEEK_API_KEY",
			},
			"openai": {
				Kind:    "openai",
				BaseURL: "https://api.openai.com/v1",
				Model:   "gpt-5.6",
				KeyEnv:  "OPENAI_API_KEY",
				// gpt-5.6-sol — рассуждающая модель: max_tokens объявлен
				// устаревшим и несовместим с ней, нужен max_completion_tokens.
				Reasoning: true,
			},
			"openrouter": {
				Kind:    "openai",
				BaseURL: "https://openrouter.ai/api/v1",
				Model:   "anthropic/claude-opus-5",
				KeyEnv:  "OPENROUTER_API_KEY",
			},
		},
	}
}

// Load накладывает JSON из path поверх встроенных профилей.
// Отсутствие файла — не ошибка: встроенных достаточно.
func Load(path string) (Config, error) {
	base := Builtin()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return base, nil
	}
	if err != nil {
		return base, fmt.Errorf("конфиг %s: %w", path, err)
	}

	var over Config
	if err := json.Unmarshal(data, &over); err != nil {
		return base, fmt.Errorf("конфиг %s: %w", path, err)
	}
	if over.Default != "" {
		base.Default = over.Default
	}
	for name, p := range over.Profiles {
		cur, ok := base.Profiles[name]
		if !ok {
			base.Profiles[name] = p
			continue
		}
		if p.Kind != "" {
			cur.Kind = p.Kind
		}
		if p.BaseURL != "" {
			cur.BaseURL = p.BaseURL
		}
		if p.Model != "" {
			cur.Model = p.Model
		}
		if p.KeyEnv != "" {
			cur.KeyEnv = p.KeyEnv
		}
		// Reasoning накладывается только если поле в файле присутствует:
		// иначе опущенное поле неотличимо от явного false и флаг нельзя
		// было бы выключить.
		if p.Reasoning != nil {
			cur.Reasoning = *p.Reasoning
		}
		base.Profiles[name] = cur
	}
	return base, nil
}

func (c Config) Resolve(name string) (Profile, error) {
	if name == "" {
		name = c.Default
	}
	p, ok := c.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("неизвестный профиль %q", name)
	}
	// Профиль, заведённый пользователем в файле, мог приехать без kind:
	// без проверки он молча дойдёт до выбора провайдера и упадёт там.
	if p.Kind != KindAnthropic && p.Kind != KindOpenAI {
		return Profile{}, fmt.Errorf(
			"профиль %s: kind должен быть %q или %q, а не %q",
			name, KindAnthropic, KindOpenAI, p.Kind)
	}
	p.Name = name
	return p, nil
}

func (p Profile) APIKey() (string, error) {
	key := os.Getenv(p.KeyEnv)
	if key == "" {
		return "", fmt.Errorf("профиль %s: не задана переменная %s",
			p.Name, p.KeyEnv)
	}
	return key, nil
}
```

- [ ] **Шаг 5: Запустить тест, убедиться что проходит**

Выполнить: `go test ./internal/config/ -v`
Ожидается: PASS, 7 тестов

- [ ] **Шаг 6: Сверено с документацией 31 августа 2026**

Открытый пункт 9.2 спеки закрыт, идентификаторы выше проверены у первоисточников:

- **OpenAI**: `gpt-5.6` — действующий алиас на флагман `gpt-5.6-sol`. Модели
  `gpt-5.1`, стоявшей в первой редакции плана, не существует;
- **Deepseek**: линейка — `deepseek-v4-flash`, `deepseek-v4-pro`,
  `deepseek-v4-flash-vision-exp`. **Все три поддерживают вызов функций**,
  так что опасение насчёт рассуждающей модели снято. В профиле стоит
  `deepseek-v4-pro`; `deepseek-v4-flash` — дешёвая замена, упомянуть в README.
  Идентификаторов `deepseek-chat` и `deepseek-reasoner` в действующей
  документации нет.

Ничего проверять на этом шаге не нужно — данные уже внесены. Если при работе
обнаружится расхождение с живым API, сообщить, а не подгонять.

- [ ] **Шаг 7: Коммит**

```bash
git add go.mod go.sum internal/config/
git commit -m "Скелет Go-модуля и профили провайдеров"
```

---

## Task 8: Инструмент поиска по корпусу

**Файлы:**
- Создать: `internal/corpus/frontmatter.go`
- Создать: `internal/corpus/search.go`
- Тест: `internal/corpus/frontmatter_test.go`
- Тест: `internal/corpus/search_test.go`

**Интерфейсы:**
- Отдаёт:
  - `corpus.Chapter{Work, WorkSlug, Book, Group, Authenticity, Translator, Cite,
    Text, Path string; Number int}`;
  - `corpus.ParseChapter(path string, data []byte) (Chapter, error)`;
  - `corpus.Hit{Chapter Chapter; Score float64}`;
  - `corpus.Searcher{Collection, Root string; Run func(ctx, args ...string) ([]byte, error)}`;
  - `(*Searcher) Search(ctx context.Context, query, group string, k int) ([]Hit, error)` —
    отбрасывает `spurious`, фильтрует по `group`, если он непуст.

Frontmatter пишет наш же `emit.py` и он плоский, поэтому разбирается вручную:
YAML-библиотека в зависимости не добавляется.

- [ ] **Шаг 1: Написать падающий тест на frontmatter**

```go
// internal/corpus/frontmatter_test.go
package corpus

import "testing"

const chapterFile = `---
work: "Nicomachean Ethics"
work_slug: nicomachean-ethics
book: "I"
chapter: 7
group: ethics
authenticity: genuine
translator: "W. D. Ross"
source: mit
cite: "Nicomachean Ethics I.7"
---

The good of man is an activity of soul.

Second paragraph.
`

func TestParseChapter(t *testing.T) {
	c, err := ParseChapter("nicomachean-ethics/i/07.md", []byte(chapterFile))
	if err != nil {
		t.Fatal(err)
	}
	if c.Work != "Nicomachean Ethics" {
		t.Errorf("Work = %q", c.Work)
	}
	if c.Book != "I" {
		t.Errorf("Book = %q", c.Book)
	}
	if c.Number != 7 {
		t.Errorf("Number = %d", c.Number)
	}
	if c.Authenticity != "genuine" {
		t.Errorf("Authenticity = %q", c.Authenticity)
	}
	if c.Cite != "Nicomachean Ethics I.7" {
		t.Errorf("Cite = %q", c.Cite)
	}
	if c.Path != "nicomachean-ethics/i/07.md" {
		t.Errorf("Path = %q", c.Path)
	}
	want := "The good of man is an activity of soul.\n\nSecond paragraph."
	if c.Text != want {
		t.Errorf("Text = %q", c.Text)
	}
}

func TestParseChapterNullBook(t *testing.T) {
	src := "---\nwork: \"Poetics\"\nwork_slug: poetics\nbook: null\n" +
		"chapter: 6\ngroup: rhetoric-poetics\nauthenticity: genuine\n" +
		"translator: null\nsource: mit\ncite: \"Poetics 6\"\n---\n\ntext\n"
	c, err := ParseChapter("poetics/06.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if c.Book != "" {
		t.Errorf("Book = %q, ожидалась пустая строка", c.Book)
	}
	if c.Translator != "" {
		t.Errorf("Translator = %q", c.Translator)
	}
}

func TestParseChapterRejectsMissingFrontmatter(t *testing.T) {
	if _, err := ParseChapter("x.md", []byte("просто текст")); err == nil {
		t.Fatal("ожидалась ошибка при отсутствии frontmatter")
	}
}
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/corpus/ -run Frontmatter -v`
Ожидается: FAIL, `undefined: ParseChapter`

- [ ] **Шаг 3: Написать разбор frontmatter**

```go
// internal/corpus/frontmatter.go
package corpus

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Chapter — одна глава корпуса вместе с её метаданными.
type Chapter struct {
	Work         string
	WorkSlug     string
	Book         string
	Number       int
	Group        string
	Authenticity string
	Translator   string
	Source       string
	Cite         string
	Text         string
	Path         string
}

const delim = "---"

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if v == "null" || v == "" {
		return ""
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
		return v[1 : len(v)-1]
	}
	return v
}

// ParseChapter разбирает файл главы. Frontmatter плоский — его пишет
// build/emit.py, — поэтому YAML-библиотека не нужна.
func ParseChapter(path string, data []byte) (Chapter, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	if !sc.Scan() || strings.TrimSpace(sc.Text()) != delim {
		return Chapter{}, fmt.Errorf("%s: нет frontmatter", path)
	}

	c := Chapter{Path: path}
	closed := false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == delim {
			closed = true
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = unquote(value)
		switch strings.TrimSpace(key) {
		case "work":
			c.Work = value
		case "work_slug":
			c.WorkSlug = value
		case "book":
			c.Book = value
		case "chapter":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Chapter{}, fmt.Errorf("%s: chapter %q: %w", path, value, err)
			}
			c.Number = n
		case "group":
			c.Group = value
		case "authenticity":
			c.Authenticity = value
		case "translator":
			c.Translator = value
		case "source":
			c.Source = value
		case "cite":
			c.Cite = value
		}
	}
	if !closed {
		return Chapter{}, fmt.Errorf("%s: frontmatter не закрыт", path)
	}

	var body strings.Builder
	for sc.Scan() {
		body.WriteString(sc.Text())
		body.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return Chapter{}, fmt.Errorf("%s: %w", path, err)
	}
	c.Text = strings.TrimSpace(body.String())
	if c.Cite == "" {
		return Chapter{}, fmt.Errorf("%s: пустое поле cite", path)
	}
	return c, nil
}
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `go test ./internal/corpus/ -run Chapter -v`
Ожидается: PASS, 3 теста

- [ ] **Шаг 5: Написать падающий тест на поиск**

```go
// internal/corpus/search_test.go
package corpus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeChapter(t *testing.T, root, rel, authenticity, group, cite string) {
	t.Helper()
	body := "---\nwork: \"W\"\nwork_slug: w\nbook: \"I\"\nchapter: 1\n" +
		"group: " + group + "\nauthenticity: " + authenticity +
		"\ntranslator: null\nsource: mit\ncite: \"" + cite + "\"\n---\n\nтело главы\n"
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeQmd(out string) func(context.Context, ...string) ([]byte, error) {
	return func(context.Context, ...string) ([]byte, error) {
		return []byte(out), nil
	}
}

func TestSearchDropsSpurious(t *testing.T) {
	root := t.TempDir()
	writeChapter(t, root, "ne/i/01.md", "genuine", "ethics", "NE I.1")
	writeChapter(t, root, "econ/i/01.md", "spurious", "ethics", "Economics I.1")

	s := &Searcher{
		Collection: "aristotle",
		Root:       root,
		Run: fakeQmd(`[
		  {"file":"qmd://aristotle/ne/i/01.md","score":0.9},
		  {"file":"qmd://aristotle/econ/i/01.md","score":0.8}
		]`),
	}
	hits, err := s.Search(context.Background(), "happiness", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("получено %d попаданий, ожидалось 1", len(hits))
	}
	if hits[0].Chapter.Cite != "NE I.1" {
		t.Fatalf("Cite = %q", hits[0].Chapter.Cite)
	}
}

func TestSearchFiltersByGroup(t *testing.T) {
	root := t.TempDir()
	writeChapter(t, root, "ne/i/01.md", "genuine", "ethics", "NE I.1")
	writeChapter(t, root, "ha/i/01.md", "genuine", "biology", "HA I.1")

	s := &Searcher{Collection: "aristotle", Root: root, Run: fakeQmd(`[
	  {"file":"qmd://aristotle/ne/i/01.md","score":0.9},
	  {"file":"qmd://aristotle/ha/i/01.md","score":0.85}
	]`)}
	hits, err := s.Search(context.Background(), "х", "ethics", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chapter.Group != "ethics" {
		t.Fatalf("фильтр по group не сработал: %+v", hits)
	}
}

func TestSearchPassesCollectionAndLimit(t *testing.T) {
	var got []string
	s := &Searcher{Collection: "aristotle", Root: t.TempDir(),
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			got = args
			return []byte("[]"), nil
		}}
	if _, err := s.Search(context.Background(), "happiness", "", 6); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"query", "happiness", "-c aristotle", "--json", "-n 6"} {
		if !strings.Contains(joined, strings.ReplaceAll(want, " ", " ")) {
			t.Errorf("в аргументах нет %q: %v", want, got)
		}
	}
}

func TestSearchSkipsMissingFile(t *testing.T) {
	s := &Searcher{Collection: "aristotle", Root: t.TempDir(),
		Run: fakeQmd(`[{"file":"qmd://aristotle/нет/такого.md","score":0.9}]`)}
	hits, err := s.Search(context.Background(), "x", "", 5)
	if err != nil {
		t.Fatalf("пропавший файл не должен ронять поиск: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("получено %d попаданий", len(hits))
	}
}
```

- [ ] **Шаг 6: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/corpus/ -run Search -v`
Ожидается: FAIL, `undefined: Searcher`

- [ ] **Шаг 7: Написать поиск**

```go
package corpus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Hit struct {
	Chapter Chapter
	Score   float64
}

type qmdResult struct {
	File  string  `json:"file"`
	Score float64 `json:"score"`
}

// Searcher — обёртка над qmd. Run вынесен полем, чтобы тесты не запускали
// внешний процесс.
type Searcher struct {
	Collection string
	Root       string
	Run        func(ctx context.Context, args ...string) ([]byte, error)
}

func RunQmd(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "qmd", args...).Output()
}

// Search спрашивает qmd, дочитывает главы с диска и отсеивает неподлинное.
// qmd get не используется: он нумерует строки и игнорирует --json,
// а нумерация сломала бы сверку выдержек.
func (s *Searcher) Search(ctx context.Context, query, group string, k int) ([]Hit, error) {
	if k <= 0 {
		k = 6
	}
	run := s.Run
	if run == nil {
		run = RunQmd
	}

	out, err := run(ctx, "query", query, "-c", s.Collection,
		"--json", "-n", strconv.Itoa(k))
	if err != nil {
		return nil, fmt.Errorf("qmd query: %w", err)
	}

	var results []qmdResult
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, fmt.Errorf("разбор выдачи qmd: %w", err)
	}

	prefix := "qmd://" + s.Collection + "/"
	hits := make([]Hit, 0, len(results))
	read := 0
	parsed := 0
	for _, r := range results {
		rel := strings.TrimPrefix(r.File, prefix)

		joined := filepath.Join(s.Root, rel)
		safe, err := filepath.Rel(s.Root, joined)
		if err != nil || safe == ".." || strings.HasPrefix(safe, ".."+string(filepath.Separator)) {
			continue // выдача qmd пытается увести чтение за пределы корня корпуса
		}

		data, err := os.ReadFile(joined)
		if err != nil {
			continue // индекс отстал от диска — не роняем беседу
		}
		read++

		ch, err := ParseChapter(rel, data)
		if err != nil {
			continue // главы читаются, но эта не разбирается — не роняем беседу целиком
		}
		parsed++

		if ch.Authenticity == "spurious" {
			continue
		}
		if group != "" && ch.Group != group {
			continue
		}
		hits = append(hits, Hit{Chapter: ch, Score: r.Score})
	}
	// Разделяем два разных отказа от честной пустоты. Оба счётчика растут до
	// фильтров spurious/group, поэтому выдача, целиком отсеянная фильтром —
	// все главы подлинные и разобранные, просто не той группы, — законная
	// пустота и сюда не попадает.
	switch {
	case len(results) > 0 && read == 0:
		// Ни одна глава не прочиталась с диска — почти наверняка неверно
		// указан корень корпуса.
		return nil, fmt.Errorf(
			"qmd вернул %d попаданий, но ни одна глава не прочиталась с диска; "+
				"похоже, неверный корень корпуса: %s", len(results), s.Root)
	case read > 0 && parsed == 0:
		// Главы читаются, но ни одна не разбирается — похоже, корпус собран
		// не тем конвейером, или его схема разошлась с ожидаемой.
		return nil, fmt.Errorf(
			"qmd вернул %d попаданий, главы читаются, но ни одна не разобралась; "+
				"похоже, корпус собран не тем конвейером или разошёлся со схемой", len(results))
	}
	// Пропуск отдельных нечитаемых или неразбираемых файлов при том, что
	// другие прочлись и разобрались, остаётся молчаливым: индекс мог просто
	// отстать от диска. Выдача, целиком отсеянная фильтрами spurious/group,
	// — тоже законная пустота, а не сбой.
	return hits, nil
}
```

**Реализация укреплена сверх первой редакции по итогам ревью**, и код выше —
поставленный, а не эталонный:

- проверяются все обязательные поля frontmatter, а не только `cite`. `book`
  и `translator` обязательными не считаются: в схеме они законно бывают
  пустыми;
- путь из выдачи qmd удерживается в границах корня через `filepath.Rel` —
  значение с `..` не уводит чтение наружу;
- два счётчика вместо молчаливых пропусков разделяют четыре исхода:
  ничего не прочлось (ошибка, неверный корень), прочлось но не разобралось
  (ошибка, негодный корпус), разобралось но отсеяно фильтрами (законная
  пустота), qmd не нашёл ничего (законная пустота). Оба счётчика растут
  **до** фильтров: иначе выдача, целиком отсеянная по группе, стала бы
  ошибкой.

Тестов в пакете 18.

- [ ] **Шаг 8: Запустить все тесты пакета**

Выполнить: `go test ./internal/corpus/ -v`
Ожидается: PASS, 7 тестов

- [ ] **Шаг 9: Проверить на живом корпусе**

Выполнить:
```bash
cat > /tmp/smoke_corpus.go <<'EOF'
package main

import (
	"context"
	"fmt"
	"github.com/alex0x12/aristotle/internal/corpus"
)

func main() {
	s := &corpus.Searcher{Collection: "aristotle", Root: "/Development/aristotle/corpus"}
	hits, err := s.Search(context.Background(), "what is happiness for man", "", 5)
	if err != nil {
		panic(err)
	}
	for _, h := range hits {
		fmt.Printf("%.2f  %-32s  %s\n", h.Score, h.Chapter.Cite, h.Chapter.Path)
	}
}
EOF
go run /tmp/smoke_corpus.go
```
Ожидается: пять строк с ссылками; среди них `Nicomachean Ethics I.7`.
Заодно засечь время — это вход в замер из Task 16.

- [ ] **Шаг 10: Коммит**

```bash
git add internal/corpus/
git commit -m "Разбор глав и поиск по корпусу с отсевом псевдо-Аристотеля"
```

---

## Task 9: Потоковый разбор маркеров

Спека 5.3. Маркеров ровно шесть, они строчные: `>> cite:`, `>> en:`, `>> ru:`,
`>> вывод:`, `>> [факт]:`, `>> [посылка]:`. Отсюда устройство разборщика —
он копит дельты до перевода строки и классифицирует **готовую строку**.
Строка с `>>`, не совпавшая ни с одним маркером, идёт как обычный текст:
ронять беседу из-за опечатки модели незачем.

**Файлы:**
- Создать: `internal/render/markers.go`
- Тест: `internal/render/markers_test.go`

**Интерфейсы:**
- Отдаёт:
  - `render.Kind` с константами `Prose`, `Cite`, `En`, `Ru`, `Inference`,
    `FactNote`, `PremiseNote`;
  - `render.Event{Kind Kind; Text string}`;
  - `render.Parser` с методами `Write(delta string) []Event` и `Flush() []Event`.

- [ ] **Шаг 1: Написать падающий тест**

```go
// internal/render/markers_test.go
package render

import "testing"

func collect(p *Parser, deltas ...string) []Event {
	var out []Event
	for _, d := range deltas {
		out = append(out, p.Write(d)...)
	}
	return append(out, p.Flush()...)
}

func TestProseLine(t *testing.T) {
	got := collect(&Parser{}, "Начнём с явлений.\n")
	if len(got) != 1 || got[0].Kind != Prose {
		t.Fatalf("%+v", got)
	}
	if got[0].Text != "Начнём с явлений." {
		t.Fatalf("Text = %q", got[0].Text)
	}
}

func TestQuoteTriplet(t *testing.T) {
	got := collect(&Parser{},
		">> cite: Nicomachean Ethics I.7\n",
		">> en: the good of man is an activity of soul\n",
		">> ru: благо человека есть деятельность души\n")
	if len(got) != 3 {
		t.Fatalf("получено %d событий: %+v", len(got), got)
	}
	if got[0].Kind != Cite || got[0].Text != "Nicomachean Ethics I.7" {
		t.Errorf("cite: %+v", got[0])
	}
	if got[1].Kind != En || got[1].Text != "the good of man is an activity of soul" {
		t.Errorf("en: %+v", got[1])
	}
	if got[2].Kind != Ru {
		t.Errorf("ru: %+v", got[2])
	}
}

func TestRussianMarkers(t *testing.T) {
	got := collect(&Parser{},
		">> вывод: этого я не писал\n",
		">> [факт]: самозарождения нет\n",
		">> [посылка]: держится на природном различии\n")
	want := []Kind{Inference, FactNote, PremiseNote}
	for i, k := range want {
		if got[i].Kind != k {
			t.Errorf("событие %d: Kind = %v, ожидался %v", i, got[i].Kind, k)
		}
	}
	if got[0].Text != "этого я не писал" {
		t.Errorf("Text = %q", got[0].Text)
	}
}

func TestSplitAcrossDeltas(t *testing.T) {
	got := collect(&Parser{}, ">> ci", "te: Nicomache", "an Ethics I.7\n")
	if len(got) != 1 || got[0].Kind != Cite {
		t.Fatalf("%+v", got)
	}
	if got[0].Text != "Nicomachean Ethics I.7" {
		t.Fatalf("Text = %q", got[0].Text)
	}
}

func TestUnknownMarkerIsProse(t *testing.T) {
	got := collect(&Parser{}, ">> цитата: опечатка модели\n")
	if len(got) != 1 || got[0].Kind != Prose {
		t.Fatalf("%+v", got)
	}
	if got[0].Text != ">> цитата: опечатка модели" {
		t.Fatalf("строка должна пройти как есть, получено %q", got[0].Text)
	}
}

func TestFlushEmitsTailLine(t *testing.T) {
	p := &Parser{}
	p.Write("хвост без перевода строки")
	got := p.Flush()
	if len(got) != 1 || got[0].Text != "хвост без перевода строки" {
		t.Fatalf("%+v", got)
	}
}

func TestFlushTwiceEmitsNothing(t *testing.T) {
	p := &Parser{}
	p.Write("хвост")
	p.Flush()
	if got := p.Flush(); len(got) != 0 {
		t.Fatalf("повторный Flush выдал %+v", got)
	}
}

func TestBlankLinePreserved(t *testing.T) {
	got := collect(&Parser{}, "первый\n", "\n", "второй\n")
	if len(got) != 3 {
		t.Fatalf("пустая строка должна сохраниться: %+v", got)
	}
	if got[1].Text != "" || got[1].Kind != Prose {
		t.Fatalf("%+v", got[1])
	}
}
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/render/ -v`
Ожидается: FAIL, `undefined: Parser`

- [ ] **Шаг 3: Написать минимальную реализацию**

```go
// internal/render/markers.go
package render

import "strings"

type Kind int

const (
	Prose Kind = iota
	Cite
	En
	Ru
	Inference
	FactNote
	PremiseNote
)

func (k Kind) String() string {
	switch k {
	case Cite:
		return "cite"
	case En:
		return "en"
	case Ru:
		return "ru"
	case Inference:
		return "вывод"
	case FactNote:
		return "факт"
	case PremiseNote:
		return "посылка"
	default:
		return "проза"
	}
}

type Event struct {
	Kind Kind
	Text string
}

// markers — закрытый набор. Порядок важен только для читаемости.
var markers = []struct {
	prefix string
	kind   Kind
}{
	{">> cite:", Cite},
	{">> en:", En},
	{">> ru:", Ru},
	{">> вывод:", Inference},
	{">> [факт]:", FactNote},
	{">> [посылка]:", PremiseNote},
}

// Parser превращает поток текстовых дельт в поток строчных событий.
// Маркеры строчные, поэтому копим до перевода строки.
type Parser struct {
	buf strings.Builder
}

func classify(line string) Event {
	for _, m := range markers {
		if strings.HasPrefix(line, m.prefix) {
			return Event{
				Kind: m.kind,
				Text: strings.TrimSpace(strings.TrimPrefix(line, m.prefix)),
			}
		}
	}
	return Event{Kind: Prose, Text: line}
}

func (p *Parser) Write(delta string) []Event {
	p.buf.WriteString(delta)
	s := p.buf.String()

	var events []Event
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			break
		}
		events = append(events, classify(strings.TrimRight(s[:i], "\r")))
		s = s[i+1:]
	}

	p.buf.Reset()
	p.buf.WriteString(s)
	return events
}

// Flush отдаёт остаток без перевода строки. Повторный вызов пуст.
func (p *Parser) Flush() []Event {
	rest := p.buf.String()
	p.buf.Reset()
	if rest == "" {
		return nil
	}
	return []Event{classify(rest)}
}
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `go test ./internal/render/ -v`
Ожидается: PASS, 8 тестов

- [ ] **Шаг 5: Коммит**

```bash
git add internal/render/markers.go internal/render/markers_test.go
git commit -m "Потоковый разбор маркеров ответа"
```

---

## Task 10: Сверка выдержек

Спека 5.2. Два шага: нормализованный поиск подстроки, при промахе — доля слов
выдержки, встретившихся в одной главе, с порогом 90 %. Второй шаг нужен потому,
что модель обрывает цитату многоточием или склеивает соседние предложения.

**Файлы:**
- Создать: `internal/render/verify.go`
- Тест: `internal/render/verify_test.go`

**Интерфейсы:**
- Потребляет: ничего из других пакетов — намеренно, чтобы сверка тестировалась
  в отрыве от сети и корпуса.
- Отдаёт:
  - `render.Normalize(s string) string`;
  - `render.Verify(quote string, chapters []string) bool`;
  - константа `render.Threshold = 0.9`.

- [ ] **Шаг 1: Написать падающий тест**

```go
// internal/render/verify_test.go
package render

import "testing"

const passage = "But the good of man turns out to be an activity of soul " +
	"in accordance with virtue, and if there are more than one virtue, " +
	"in accordance with the best and most complete."

func TestNormalizeStripsCaseAndPunctuation(t *testing.T) {
	if got := Normalize("The Good,  of Man!"); got != "the good of man" {
		t.Fatalf("Normalize = %q", got)
	}
}

func TestVerifyExactSubstring(t *testing.T) {
	q := "an activity of soul in accordance with virtue"
	if !Verify(q, []string{passage}) {
		t.Fatal("точная подстрока должна находиться")
	}
}

func TestVerifyIgnoresCaseAndPunctuation(t *testing.T) {
	q := "An Activity of Soul, in accordance with Virtue!"
	if !Verify(q, []string{passage}) {
		t.Fatal("нормализация должна снимать регистр и пунктуацию")
	}
}

func TestVerifyAcceptsElidedQuote(t *testing.T) {
	q := "the good of man turns out to be an activity of soul ... " +
		"in accordance with the best and most complete"
	if !Verify(q, []string{passage}) {
		t.Fatal("оборванная многоточием цитата должна проходить по словам")
	}
}

func TestVerifyRejectsFabrication(t *testing.T) {
	q := "happiness is the reward of a life devoted to contemplation of the stars"
	if Verify(q, []string{passage}) {
		t.Fatal("выдуманная цитата не должна проходить")
	}
}

func TestVerifyAcrossSeveralChapters(t *testing.T) {
	q := "an activity of soul in accordance with virtue"
	if !Verify(q, []string{"нерелевантная глава", passage}) {
		t.Fatal("искать надо во всех выдержках")
	}
}

func TestVerifyRejectsWordsSpreadAcrossChapters(t *testing.T) {
	a := "the good of man turns out to be"
	b := "an activity of soul in accordance with virtue"
	q := "the good of man turns out to be an activity of soul in accordance with virtue"
	if Verify(q, []string{a, b}) {
		t.Fatal("слова из разных глав не образуют цитату")
	}
}

func TestVerifyEmptyQuoteIsFalse(t *testing.T) {
	if Verify("   ", []string{passage}) {
		t.Fatal("пустая выдержка не подтверждается")
	}
}

func TestVerifyNoChaptersIsFalse(t *testing.T) {
	if Verify("an activity of soul", nil) {
		t.Fatal("без выдержек подтверждать нечем")
	}
}
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/render/ -run Verify -v`
Ожидается: FAIL, `undefined: Verify`

- [ ] **Шаг 3: Написать минимальную реализацию**

```go
// internal/render/verify.go
package render

import (
	"strings"
	"unicode"
)

// Threshold — доля слов выдержки, которую надо найти в одной главе,
// если точная подстрока не совпала. Устанавливается в 1.0 потому, что элизия
// (обрывание многоточием) выбрасывает слова главы, а не слова цитаты.
// Подлинная выдержка, пропускающая слова в исходном тексте, должна содержать
// только слова, которые действительно там есть. Допуск в 0.9 позволял бы модели
// вставлять в цитату выдуманные слова.
const Threshold = 1.0

// Normalize приводит текст к виду, в котором сравнение не зависит
// от регистра, пунктуации и переносов.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		default:
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// Verify отвечает, подтверждается ли выдержка хотя бы одной из глав,
// которые поиск реально вернул на этом ходу.
func Verify(quote string, chapters []string) bool {
	q := Normalize(quote)
	if q == "" || len(chapters) == 0 {
		return false
	}

	quoteWords := strings.Fields(q)
	if len(quoteWords) == 0 {
		return false
	}

	// Шаг 1: поиск точной подстроки.
	for _, c := range chapters {
		n := Normalize(c)
		if strings.Contains(n, q) {
			return true
		}
	}

	// Шаг 2: если цитата короче 6 слов, точное вхождение обязательно.
	if len(quoteWords) < 6 {
		return false
	}

	// Шаг 2: жадный поиск подпоследовательности с сохранением порядка.
	// Слова считаются только внутри одной главы: набор из разных глав
	// цитатой не является.
	for _, c := range chapters {
		n := Normalize(c)
		chapterWords := strings.Fields(n)

		// Жадный поиск подпоследовательности.
		quoteIdx := 0
		chapterIdx := 0

		for chapterIdx < len(chapterWords) && quoteIdx < len(quoteWords) {
			if chapterWords[chapterIdx] == quoteWords[quoteIdx] {
				quoteIdx++
			}
			chapterIdx++
		}

		hit := quoteIdx
		if float64(hit)/float64(len(quoteWords)) >= Threshold {
			return true
		}
	}
	return false
}
```

**Алгоритм переписан по итогам состязательного ревью**, и код выше —
поставленный, а не эталонный. Первая редакция считала долю слов цитаты,
**встречающихся** в главе, как множество — без порядка и без учёта повторов.
Ревью пробило её эмпирически четырьмя способами: цитата из служебных слов
набирала 100 %, переставленная лексика главы — 100 %, повтор одного слова
десять раз проходил против главы из одного этого слова, а на двух словах
шкала вырождалась в 0/50/100 и порог терял смысл.

Причина одна: цитата, оборванная многоточием, — это не набор слов из главы,
а **слова главы по порядку с пропусками**. Поэтому вместо принадлежности
множеству идёт жадный поиск подпоследовательности: два указателя, найденная
позиция главы потребляется и больше не переиспользуется, промах указатель
не двигает. Это чинит порядок, повторы и подбор служебных слов разом,
а пропуски между совпадениями — ровно то, чем и является многоточие.
Ниже шести нормализованных слов нечёткий шаг не применяется вовсе:
требуется точное вхождение.

**Порог поднят до 1.0**, и это вторая правка того же рассуждения. Допуск
в десять процентов закладывался ради оборванной многоточием цитаты — и был
там не нужен: многоточие выбрасывает слова **главы**, а не слова цитаты,
поэтому подлинная выдержка обязана совпасть целиком. Допуск не покупал ни
одного законного случая, зато покупал право модели подмешать отсебятину:
цитата из двенадцати настоящих слов плюс одно выдуманное набирала 92 %
и проходила.

Принятый остаточный риск: модель, целенаправленно собирающая фальшивку из
слов главы, разбросанных по тексту, но идущих по порядку, сверку обойдёт.
Угроза проекта — небрежность модели, а не злой умысел; свободно
сгенерированная выдумка точной подпоследовательностью главы не бывает.

Тестов в пакете 26.

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `go test ./internal/render/ -v`
Ожидается: PASS, все тесты пакета (маркеры + сверка)

- [ ] **Шаг 5: Коммит**

```bash
git add internal/render/verify.go internal/render/verify_test.go
git commit -m "Механическая сверка выдержек с полученными главами"
```

---

## Task 11: Интерфейс провайдера и ветка Anthropic

Спека 6.1–6.2. Нейтральность живёт в собственных типах: сводить Claude
к OpenAI-совместимому шиму нельзя, теряются кэш и мышление.

**Файлы:**
- Создать: `internal/provider/provider.go`
- Создать: `internal/provider/anthropic/anthropic.go`
- Тест: `internal/provider/provider_test.go`

**Интерфейсы:**
- Отдаёт:
  - `provider.Role` со значениями `RoleUser`, `RoleAssistant`;
  - `provider.Message{Role Role; Text string; ToolCalls []ToolCall; ToolResults []ToolResult}`;
  - `provider.ToolCall{ID, Name string; Input map[string]any}`;
  - `provider.ToolResult{ID string; Content string; IsError bool}`;
  - `provider.ToolDef{Name, Description string; Schema map[string]any}`;
  - `provider.Request{System string; Messages []Message; Tools []ToolDef; MaxTokens int}`;
  - `provider.EventKind` со значениями `EventText`, `EventToolCall`, `EventDone`;
  - `provider.Event{Kind EventKind; Text string; Call ToolCall; StopReason string}`;
  - `provider.Stream` — интерфейс `Next() bool`, `Event() Event`, `Err() error`, `Close() error`;
  - `provider.Provider` — интерфейс `Stream(ctx, Request) (Stream, error)`;
  - `anthropic.New(profile config.Profile) (provider.Provider, error)`.

- [ ] **Шаг 1: Написать типы и падающий тест**

```go
// internal/provider/provider_test.go
package provider

import "testing"

func TestSearchToolSchemaIsStable(t *testing.T) {
	// Определение инструмента входит в кэшируемый префикс (tools -> system ->
	// messages), поэтому оно обязано быть неизменным от запроса к запросу.
	a := SearchCorpusTool()
	b := SearchCorpusTool()
	if a.Name != b.Name || a.Description != b.Description {
		t.Fatal("определение инструмента должно быть детерминированным")
	}
	props, ok := a.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("нет properties: %+v", a.Schema)
	}
	for _, key := range []string{"query", "group", "k"} {
		if _, ok := props[key]; !ok {
			t.Errorf("в схеме нет параметра %q", key)
		}
	}
	req, ok := a.Schema["required"].([]string)
	if !ok || len(req) != 1 || req[0] != "query" {
		t.Errorf("required = %+v, ожидался [query]", a.Schema["required"])
	}
}

func TestRequestDefaultsMaxTokens(t *testing.T) {
	r := Request{}
	if got := r.EffectiveMaxTokens(); got != DefaultMaxTokens {
		t.Fatalf("EffectiveMaxTokens = %d, ожидалось %d", got, DefaultMaxTokens)
	}
	r.MaxTokens = 1000
	if got := r.EffectiveMaxTokens(); got != 1000 {
		t.Fatalf("EffectiveMaxTokens = %d", got)
	}
}
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/provider/ -v`
Ожидается: FAIL, `undefined: SearchCorpusTool`

- [ ] **Шаг 3: Написать нейтральные типы**

```go
// internal/provider/provider.go
package provider

import "context"

// DefaultMaxTokens — при стриминге занижать нечего, таймаутов тут не бывает.
const DefaultMaxTokens = 64000

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type ToolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

type ToolResult struct {
	ID      string
	Content string
	IsError bool
}

type Message struct {
	Role        Role
	Text        string
	ToolCalls   []ToolCall
	ToolResults []ToolResult
}

type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
}

type Request struct {
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}

func (r Request) EffectiveMaxTokens() int {
	if r.MaxTokens > 0 {
		return r.MaxTokens
	}
	return DefaultMaxTokens
}

type EventKind int

const (
	EventText EventKind = iota
	EventToolCall
	EventDone
)

type Event struct {
	Kind       EventKind
	Text       string
	Call       ToolCall
	StopReason string
}

type Stream interface {
	Next() bool
	Event() Event
	Err() error
	Close() error
}

type Provider interface {
	Stream(ctx context.Context, req Request) (Stream, error)
}

const searchDescription = `Ищет в корпусе сочинений Аристотеля и возвращает главы целиком.
Запрос формулируй на английском языке корпуса и в понятиях самого Аристотеля,
а не в словах собеседника: не "burnout", а "leisure and activity" или
"the mean in exertion". Псевдо-аристотелевские сочинения из выдачи исключены.`

// SearchCorpusTool возвращает определение инструмента. Значение обязано быть
// байт-в-байт одинаковым между запросами: оно входит в кэшируемый префикс.
func SearchCorpusTool() ToolDef {
	return ToolDef{
		Name:        "search_corpus",
		Description: searchDescription,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Поисковый запрос на английском, в понятиях корпуса.",
				},
				"group": map[string]any{
					"type": "string",
					"enum": []string{
						"logic", "physics", "psychology", "biology",
						"metaphysics", "ethics", "politics", "rhetoric-poetics",
					},
					"description": "Необязательное сужение области поиска.",
				},
				"k": map[string]any{
					"type":        "integer",
					"description": "Сколько глав вернуть, по умолчанию 6.",
				},
			},
			"required": []string{"query"},
		},
	}
}
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `go test ./internal/provider/ -v`
Ожидается: PASS, 2 теста

- [ ] **Шаг 5: Написать ветку Anthropic**

Опорные факты из справочника, проверенные для Go SDK: у стрима **нет**
`GetFinalMessage()` — финальное сообщение копится через `Accumulate`;
типизированной константы для Opus 5 нет, модель передаётся строкой;
на Opus 5 мышление включено по умолчанию, поле `Thinking` не трогаем;
`System` — это `[]TextBlockParam`, и `CacheControl` ставится на последнем блоке.

```go
// internal/provider/anthropic/anthropic.go
package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/provider"
)

type client struct {
	api   sdk.Client
	model string
}

func New(p config.Profile) (provider.Provider, error) {
	key, err := p.APIKey()
	if err != nil {
		return nil, err
	}
	return &client{
		api:   sdk.NewClient(option.WithAPIKey(key)),
		model: p.Model,
	}, nil
}

func (c *client) params(req provider.Request) sdk.MessageNewParams {
	p := sdk.MessageNewParams{
		Model:     sdk.Model(c.model),
		MaxTokens: int64(req.EffectiveMaxTokens()),
	}

	// Кэшируем префикс: персона не меняется в течение беседы.
	if req.System != "" {
		p.System = []sdk.TextBlockParam{{
			Text:         req.System,
			CacheControl: sdk.NewCacheControlEphemeralParam(),
		}}
	}

	for _, t := range req.Tools {
		p.Tools = append(p.Tools, sdk.ToolUnionParam{
			OfTool: &sdk.ToolParam{
				Name:        t.Name,
				Description: sdk.String(t.Description),
				InputSchema: sdk.ToolInputSchemaParam{
					Properties: t.Schema["properties"],
				},
			},
		})
	}

	for _, m := range req.Messages {
		var blocks []sdk.ContentBlockParamUnion
		if m.Text != "" {
			blocks = append(blocks, sdk.NewTextBlock(m.Text))
		}
		for _, call := range m.ToolCalls {
			blocks = append(blocks, sdk.NewToolUseBlock(call.ID, call.Input, call.Name))
		}
		for _, res := range m.ToolResults {
			blocks = append(blocks, sdk.NewToolResultBlock(res.ID, res.Content, res.IsError))
		}
		if m.Role == provider.RoleAssistant {
			p.Messages = append(p.Messages, sdk.NewAssistantMessage(blocks...))
		} else {
			p.Messages = append(p.Messages, sdk.NewUserMessage(blocks...))
		}
	}
	return p
}

func (c *client) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	raw := c.api.Messages.NewStreaming(ctx, c.params(req))
	return &stream{raw: raw, msg: sdk.Message{}}, nil
}

type stream struct {
	raw     *ssestream.Stream[sdk.MessageStreamEventUnion]
	msg     sdk.Message
	pending []provider.Event
	cur     provider.Event
	done    bool
	err     error
}

func (s *stream) Next() bool {
	if len(s.pending) > 0 {
		s.cur, s.pending = s.pending[0], s.pending[1:]
		return true
	}
	for s.raw.Next() {
		ev := s.raw.Current()
		if err := s.msg.Accumulate(ev); err != nil {
			s.err = err
			return false
		}
		if delta, ok := ev.AsAny().(sdk.ContentBlockDeltaEvent); ok {
			if td, ok := delta.Delta.AsAny().(sdk.TextDelta); ok && td.Text != "" {
				s.cur = provider.Event{Kind: provider.EventText, Text: td.Text}
				return true
			}
		}
	}
	if err := s.raw.Err(); err != nil {
		s.err = err
		return false
	}
	if s.done {
		return false
	}
	s.done = true
	s.pending = s.finish()
	if len(s.pending) == 0 {
		return false
	}
	s.cur, s.pending = s.pending[0], s.pending[1:]
	return true
}

// finish собирает вызовы инструментов из накопленного сообщения.
func (s *stream) finish() []provider.Event {
	var out []provider.Event
	for _, block := range s.msg.Content {
		use, ok := block.AsAny().(sdk.ToolUseBlock)
		if !ok {
			continue
		}
		var input map[string]any
		// Модель может по-разному экранировать JSON — всегда разбираем,
		// никогда не сравниваем сериализованную строку.
		if err := json.Unmarshal([]byte(use.JSON.Input.Raw()), &input); err != nil {
			s.err = fmt.Errorf("разбор аргументов инструмента: %w", err)
			return nil
		}
		out = append(out, provider.Event{
			Kind: provider.EventToolCall,
			Call: provider.ToolCall{ID: use.ID, Name: use.Name, Input: input},
		})
	}
	return append(out, provider.Event{
		Kind:       provider.EventDone,
		StopReason: string(s.msg.StopReason),
	})
}

func (s *stream) Event() provider.Event { return s.cur }
func (s *stream) Err() error            { return s.err }
func (s *stream) Close() error          { return s.raw.Close() }
```

- [ ] **Шаг 6: Довести до компиляции по ошибкам компилятора**

Выполнить: `go build ./...`

Имена типов SDK — `ssestream.Stream`, `ToolUnionParam`, `ToolInputSchemaParam`,
`NewToolUseBlock`, `NewToolResultBlock`, `use.JSON.Input.Raw()` — брать
**из ошибок компилятора и из исходников SDK в модульном кэше**, а не угадывать:

```bash
go doc github.com/anthropics/anthropic-sdk-go ToolParam
go doc github.com/anthropics/anthropic-sdk-go ToolUseBlock
grep -rn "func NewToolResultBlock" "$(go env GOMODCACHE)"/github.com/anthropics/anthropic-sdk-go*/
```

Не ходить в сеть за документацией: цикл «собрать — прочитать ошибку — поправить»
здесь быстрее. Дописать импорт `ssestream` по подсказке компилятора.

- [ ] **Шаг 7: Проверить живым вызовом**

Выполнить:
```bash
cat > /tmp/smoke_anthropic.go <<'EOF'
package main

import (
	"context"
	"fmt"

	"github.com/alex0x12/aristotle/internal/config"
	anth "github.com/alex0x12/aristotle/internal/provider/anthropic"
	"github.com/alex0x12/aristotle/internal/provider"
)

func main() {
	p, _ := config.Builtin().Resolve("claude")
	cl, err := anth.New(p)
	if err != nil {
		panic(err)
	}
	st, err := cl.Stream(context.Background(), provider.Request{
		System:   "Отвечай одним коротким предложением по-русски.",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "Назови четыре причины."}},
	})
	if err != nil {
		panic(err)
	}
	defer st.Close()
	for st.Next() {
		if e := st.Event(); e.Kind == provider.EventText {
			fmt.Print(e.Text)
		}
	}
	fmt.Println()
	if err := st.Err(); err != nil {
		panic(err)
	}
}
EOF
go run /tmp/smoke_anthropic.go
```
Ожидается: текст появляется по мере генерации, а не разом в конце.

- [ ] **Шаг 8: Коммит**

```bash
git add internal/provider/
git commit -m "Нейтральный интерфейс провайдера и ветка Anthropic"
```

---

## Task 12: Ветка OpenAI-совместимых провайдеров

Спека 6.3. Одна реализация на OpenAI, Deepseek и OpenRouter.
Главная ловушка: аргументы вызова функции приходят кусками
в `delta.tool_calls[].function.arguments`, и собирать их надо **по полю `index`**,
а не по порядку прибытия.

**Файлы:**
- Создать: `internal/provider/openaicompat/openaicompat.go`
- Тест: `internal/provider/openaicompat/openaicompat_test.go`

**Интерфейсы:**
- Отдаёт: `openaicompat.New(p config.Profile) (provider.Provider, error)`
  и внутренний `accumulator` с методами `Add(index int, id, name, argsFragment string)`
  и `Calls() []provider.ToolCall` — вынесен отдельно ради тестируемости.

- [ ] **Шаг 0: Сделать `deepseek` профилем по умолчанию**

У пользователя нет ключа Anthropic API, и из четырёх ключей в окружении задан
только `DEEPSEEK_API_KEY`. Умолчание `claude` означало бы, что собранный агент
падает на первом же запуске.

В `internal/config/config.go` заменить `Default: "claude"` на
`Default: "deepseek"`. Поправить тест `TestBuiltinHasFourProfiles`, который
проверяет умолчание, и `TestResolveEmptyGivesDefault`, который ждёт `claude`.
Профили при этом все четыре остаются на месте: ветка Anthropic никуда не
девается, она включается флагом `--model claude`.

Выполнить: `go test ./internal/config/ -v`
Ожидается: PASS.

- [ ] **Шаг 1: Написать падающий тест**

```go
// internal/provider/openaicompat/openaicompat_test.go
package openaicompat

import (
	"testing"

	"github.com/alex0x12/aristotle/internal/provider"
)

func TestAccumulatorJoinsByIndex(t *testing.T) {
	var a accumulator
	// Провайдер вправе чередовать индексы: собирать по порядку прибытия нельзя.
	a.Add(0, "call_a", "search_corpus", `{"que`)
	a.Add(1, "call_b", "search_corpus", `{"query":"c`)
	a.Add(0, "", "", `ry":"happiness"}`)
	a.Add(1, "", "", `auses"}`)

	calls := a.Calls()
	if len(calls) != 2 {
		t.Fatalf("получено %d вызовов", len(calls))
	}
	if calls[0].ID != "call_a" || calls[0].Input["query"] != "happiness" {
		t.Errorf("вызов 0: %+v", calls[0])
	}
	if calls[1].ID != "call_b" || calls[1].Input["query"] != "causes" {
		t.Errorf("вызов 1: %+v", calls[1])
	}
}

func TestAccumulatorEmptyIsNil(t *testing.T) {
	var a accumulator
	if got := a.Calls(); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestAccumulatorSkipsBrokenJSON(t *testing.T) {
	var a accumulator
	a.Add(0, "call_a", "search_corpus", `{"query": не json`)
	if got := a.Calls(); len(got) != 0 {
		t.Fatalf("битый JSON не должен превращаться в вызов: %+v", got)
	}
}

func TestReasoningProfileRenamesMaxTokens(t *testing.T) {
	body := buildBody("deepseek-reasoner", true, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	if _, ok := body["max_tokens"]; ok {
		t.Error("у рассуждающей модели max_tokens не принимается")
	}
	if _, ok := body["max_completion_tokens"]; !ok {
		t.Error("нет max_completion_tokens")
	}
	if _, ok := body["temperature"]; ok {
		t.Error("temperature у рассуждающей модели не принимается")
	}
}

func TestPlainProfileUsesMaxTokens(t *testing.T) {
	body := buildBody("deepseek-chat", false, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	if _, ok := body["max_tokens"]; !ok {
		t.Error("нет max_tokens")
	}
}

func TestSystemGoesFirstAsMessage(t *testing.T) {
	body := buildBody("gpt-5.1", false, provider.Request{
		System:   "ты Аристотель",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "привет"}},
	})
	msgs := body["messages"].([]map[string]any)
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "ты Аристотель" {
		t.Fatalf("первое сообщение: %+v", msgs[0])
	}
}
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/provider/openaicompat/ -v`
Ожидается: FAIL, `undefined: accumulator`

- [ ] **Шаг 3: Написать реализацию**

```go
// internal/provider/openaicompat/openaicompat.go
package openaicompat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/provider"
)

// accumulator склеивает вызовы функций из потока. Ключ — index, потому что
// провайдер вправе чередовать куски разных вызовов.
type accumulator struct {
	byIndex map[int]*partial
}

type partial struct {
	id, name string
	args     strings.Builder
}

func (a *accumulator) Add(index int, id, name, fragment string) {
	if a.byIndex == nil {
		a.byIndex = map[int]*partial{}
	}
	p, ok := a.byIndex[index]
	if !ok {
		p = &partial{}
		a.byIndex[index] = p
	}
	if id != "" {
		p.id = id
	}
	if name != "" {
		p.name = name
	}
	p.args.WriteString(fragment)
}

func (a *accumulator) Calls() []provider.ToolCall {
	indexes := make([]int, 0, len(a.byIndex))
	for i := range a.byIndex {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)

	var out []provider.ToolCall
	for _, i := range indexes {
		p := a.byIndex[i]
		var input map[string]any
		if err := json.Unmarshal([]byte(p.args.String()), &input); err != nil {
			continue // недособранный или битый вызов пропускаем, не роняя ход
		}
		out = append(out, provider.ToolCall{ID: p.id, Name: p.name, Input: input})
	}
	return out
}

func buildBody(model string, reasoning bool, req provider.Request) map[string]any {
	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		for _, res := range m.ToolResults {
			msgs = append(msgs, map[string]any{
				"role": "tool", "tool_call_id": res.ID, "content": res.Content,
			})
		}
		if m.Text == "" && len(m.ToolCalls) == 0 {
			continue
		}
		msg := map[string]any{"role": string(m.Role), "content": m.Text}
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, c := range m.ToolCalls {
				args, _ := json.Marshal(c.Input)
				calls = append(calls, map[string]any{
					"id": c.ID, "type": "function",
					"function": map[string]any{"name": c.Name, "arguments": string(args)},
				})
			}
			msg["tool_calls"] = calls
		}
		msgs = append(msgs, msg)
	}

	body := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   true,
	}
	if reasoning {
		body["max_completion_tokens"] = req.EffectiveMaxTokens()
	} else {
		body["max_tokens"] = req.EffectiveMaxTokens()
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": t.Name, "description": t.Description,
					"parameters": t.Schema,
				},
			})
		}
		body["tools"] = tools
	}
	return body
}

type client struct {
	baseURL   string
	key       string
	model     string
	reasoning bool
	http      *http.Client
}

func New(p config.Profile) (provider.Provider, error) {
	key, err := p.APIKey()
	if err != nil {
		return nil, err
	}
	return &client{
		baseURL:   strings.TrimSuffix(p.BaseURL, "/"),
		key:       key,
		model:     p.Model,
		reasoning: p.Reasoning,
		http:      &http.Client{},
	}, nil
}

func (c *client) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	payload, err := json.Marshal(buildBody(c.model, c.reasoning, req))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.key)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(buf.String()))
	}
	return &stream{body: resp.Body, sc: bufio.NewScanner(resp.Body)}, nil
}
```

- [ ] **Шаг 4: Дописать чтение SSE**

```go
// продолжение internal/provider/openaicompat/openaicompat.go
type chunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type stream struct {
	body    interface{ Close() error }
	sc      *bufio.Scanner
	acc     accumulator
	pending []provider.Event
	cur     provider.Event
	stop    string
	done    bool
	err     error
}

func (s *stream) Next() bool {
	if len(s.pending) > 0 {
		s.cur, s.pending = s.pending[0], s.pending[1:]
		return true
	}
	for s.sc.Scan() {
		line := strings.TrimSpace(s.sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ch chunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil || len(ch.Choices) == 0 {
			continue
		}
		choice := ch.Choices[0]
		if choice.FinishReason != "" {
			s.stop = choice.FinishReason
		}
		for _, tc := range choice.Delta.ToolCalls {
			s.acc.Add(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		if choice.Delta.Content != "" {
			s.cur = provider.Event{Kind: provider.EventText, Text: choice.Delta.Content}
			return true
		}
	}
	if err := s.sc.Err(); err != nil {
		s.err = err
		return false
	}
	if s.done {
		return false
	}
	s.done = true
	for _, call := range s.acc.Calls() {
		s.pending = append(s.pending,
			provider.Event{Kind: provider.EventToolCall, Call: call})
	}
	stop := s.stop
	if stop == "tool_calls" {
		stop = "tool_use" // приводим к словарю нейтрального слоя
	}
	s.pending = append(s.pending,
		provider.Event{Kind: provider.EventDone, StopReason: stop})
	s.cur, s.pending = s.pending[0], s.pending[1:]
	return true
}

func (s *stream) Event() provider.Event { return s.cur }
func (s *stream) Err() error            { return s.err }
func (s *stream) Close() error          { return s.body.Close() }
```

- [ ] **Шаг 5: Запустить тесты, убедиться что проходят**

Выполнить: `go test ./internal/provider/... -v`
Ожидается: PASS, все тесты обоих пакетов

- [ ] **Шаг 6: Живая проверка через Deepseek**

Это **единственная ветка провайдера, которую можно проверить живым вызовом**:
ключ Anthropic отсутствует, а `DEEPSEEK_API_KEY` в окружении задан. Поэтому
проверка обязательна и от неё зависит доверие ко всему слою провайдеров.

```bash
mkdir -p .tmp_smoke && cat > .tmp_smoke/main.go <<'EOF'
package main

import (
	"context"
	"fmt"

	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/provider/openaicompat"
)

func main() {
	p, err := config.Builtin().Resolve("deepseek")
	if err != nil {
		panic(err)
	}
	cl, err := openaicompat.New(p)
	if err != nil {
		panic(err)
	}
	st, err := cl.Stream(context.Background(), provider.Request{
		System:   "Отвечай одним коротким предложением по-русски.",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "Назови четыре причины по Аристотелю."}},
	})
	if err != nil {
		panic(err)
	}
	defer st.Close()
	for st.Next() {
		if e := st.Event(); e.Kind == provider.EventText {
			fmt.Print(e.Text)
		}
	}
	fmt.Println()
	if err := st.Err(); err != nil {
		panic(err)
	}
}
EOF
go run ./.tmp_smoke && rm -rf .tmp_smoke
```

Ожидается: осмысленный русский текст, появляющийся **по мере генерации**,
а не разом в конце. Если текст приходит одним куском — стриминг не работает,
и это дефект, а не особенность провайдера.

**Вторая живая проверка, важнее первой: вызов инструмента.** Повтори прогон,
добавив в запрос `Tools: []provider.ToolDef{provider.SearchCorpusTool()}` и
задав вопрос, на который модель обязана полезть в корпус («Что ты писал
о счастье?»). Убедись, что приходит событие `EventToolCall` с непустым
`Call.Name` и разобранным `Call.Input`. Без этого весь замысел агента
не работает: инструмент поиска — единственный способ добраться до текстов.

Каталог `.tmp_smoke` в поставку не входит — удали его.

- [ ] **Шаг 6: Коммит**

```bash
git add internal/provider/openaicompat/
git commit -m "Ветка OpenAI, Deepseek и OpenRouter со склейкой вызовов по index"
```

---

## Task 13: Цикл инструмента и история

Спека 6.5 и 5.5. Потолок — 4 итерации; на потолке инструмент возвращается
с пометкой «поиск исчерпан», и модель отвечает найденным. Обрывать ход молча
нельзя: пользователь получит пустой экран.

**Файлы:**
- Создать: `internal/chat/loop.go`
- Создать: `internal/chat/history.go`
- Тест: `internal/chat/loop_test.go`
- Тест: `internal/chat/history_test.go`

**Интерфейсы:**
- Потребляет: `provider.Provider`, `provider.Request`, `corpus.Searcher`.
- Отдаёт:
  - `chat.MaxToolIterations = 4`;
  - `chat.Session{Provider provider.Provider; Search Finder; System string;
    Messages []provider.Message}`;
  - `chat.Finder` — интерфейс `Search(ctx, query, group string, k int) ([]corpus.Hit, error)`;
  - `(*Session) Turn(ctx context.Context, userText string, onText func(string)) (TurnResult, error)`;
  - `chat.TurnResult{Text string; Chapters []string; Iterations int; Exhausted bool}` —
    `Chapters` отдаётся в `render.Verify` как список реально полученных глав;
  - `chat.History{Path string}` с `Append(rec Record) error` и `Load() ([]Record, error)`;
  - `chat.Record{Time time.Time; Role string; Text string}`.

- [ ] **Шаг 1: Написать падающий тест цикла**

```go
// internal/chat/loop_test.go
package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/provider"
)

// scriptedProvider отдаёт заранее записанные потоки, по одному на запрос.
type scriptedProvider struct {
	turns [][]provider.Event
	seen  []provider.Request
}

func (p *scriptedProvider) Stream(_ context.Context, req provider.Request) (provider.Stream, error) {
	p.seen = append(p.seen, req)
	events := p.turns[0]
	p.turns = p.turns[1:]
	return &scriptedStream{events: events}, nil
}

type scriptedStream struct {
	events []provider.Event
	cur    provider.Event
}

func (s *scriptedStream) Next() bool {
	if len(s.events) == 0 {
		return false
	}
	s.cur, s.events = s.events[0], s.events[1:]
	return true
}
func (s *scriptedStream) Event() provider.Event { return s.cur }
func (s *scriptedStream) Err() error            { return nil }
func (s *scriptedStream) Close() error          { return nil }

type fakeFinder struct {
	hits  []corpus.Hit
	calls int
}

func (f *fakeFinder) Search(context.Context, string, string, int) ([]corpus.Hit, error) {
	f.calls++
	return f.hits, nil
}

func chapterHit(cite, text string) corpus.Hit {
	return corpus.Hit{Chapter: corpus.Chapter{Cite: cite, Text: text}, Score: 0.9}
}

func TestTurnWithoutTools(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventText, Text: "Начнём "},
		{Kind: provider.EventText, Text: "с явлений."},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}}}
	s := &Session{Provider: p, Search: &fakeFinder{}}

	var got strings.Builder
	res, err := s.Turn(context.Background(), "что такое счастье", func(d string) {
		got.WriteString(d)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "Начнём с явлений." {
		t.Errorf("поток = %q", got.String())
	}
	if res.Iterations != 0 {
		t.Errorf("Iterations = %d, инструмент не звали", res.Iterations)
	}
}

func TestTurnRunsToolThenAnswers(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "happiness"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{
			{Kind: provider.EventText, Text: "Благо человека есть деятельность души."},
			{Kind: provider.EventDone, StopReason: "end_turn"},
		},
	}}
	f := &fakeFinder{hits: []corpus.Hit{
		chapterHit("Nicomachean Ethics I.7", "the good of man is an activity of soul")}}
	s := &Session{Provider: p, Search: f}

	res, err := s.Turn(context.Background(), "что такое счастье", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Errorf("поиск вызван %d раз", f.calls)
	}
	if res.Iterations != 1 {
		t.Errorf("Iterations = %d", res.Iterations)
	}
	if len(res.Chapters) != 1 ||
		!strings.Contains(res.Chapters[0], "activity of soul") {
		t.Errorf("Chapters = %+v", res.Chapters)
	}
	if res.Exhausted {
		t.Error("потолок не достигнут, Exhausted должен быть false")
	}
}

func TestToolResultCarriesCite(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "happiness"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	f := &fakeFinder{hits: []corpus.Hit{
		chapterHit("Nicomachean Ethics I.7", "тело главы")}}
	s := &Session{Provider: p, Search: f}
	if _, err := s.Turn(context.Background(), "х", func(string) {}); err != nil {
		t.Fatal(err)
	}
	// Второй запрос должен нести результат инструмента с ссылкой.
	last := p.seen[len(p.seen)-1]
	var found bool
	for _, m := range last.Messages {
		for _, r := range m.ToolResults {
			if strings.Contains(r.Content, "Nicomachean Ethics I.7") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("в результате инструмента нет ссылки на главу")
	}
}

func TestEmptySearchIsReportedNotError(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "nginx"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	s := &Session{Provider: p, Search: &fakeFinder{}}
	if _, err := s.Turn(context.Background(), "х", func(string) {}); err != nil {
		t.Fatal(err)
	}
	last := p.seen[len(p.seen)-1]
	var content string
	for _, m := range last.Messages {
		for _, r := range m.ToolResults {
			content = r.Content
		}
	}
	if !strings.Contains(content, "ничего не найдено") {
		t.Fatalf("пустая выдача должна сообщаться словами, получено %q", content)
	}
}

func TestCeilingStopsLoopAndMarksExhausted(t *testing.T) {
	toolTurn := []provider.Event{
		{Kind: provider.EventToolCall, Call: provider.ToolCall{
			ID: "c", Name: "search_corpus",
			Input: map[string]any{"query": "х"}}},
		{Kind: provider.EventDone, StopReason: "tool_use"},
	}
	turns := [][]provider.Event{toolTurn, toolTurn, toolTurn, toolTurn,
		{{Kind: provider.EventText, Text: "отвечаю тем, что нашёл"},
			{Kind: provider.EventDone, StopReason: "end_turn"}}}
	p := &scriptedProvider{turns: turns}
	f := &fakeFinder{hits: []corpus.Hit{chapterHit("X I.1", "тело")}}
	s := &Session{Provider: p, Search: f}

	res, err := s.Turn(context.Background(), "х", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != MaxToolIterations {
		t.Errorf("Iterations = %d, ожидалось %d", res.Iterations, MaxToolIterations)
	}
	if !res.Exhausted {
		t.Error("на потолке Exhausted должен быть true")
	}
	last := p.seen[len(p.seen)-1]
	var content string
	for _, m := range last.Messages {
		for _, r := range m.ToolResults {
			content = r.Content
		}
	}
	if !strings.Contains(content, "поиск исчерпан") {
		t.Fatalf("на потолке модель должна получить пометку, получено %q", content)
	}
	if res.Text != "отвечаю тем, что нашёл" {
		t.Errorf("Text = %q", res.Text)
	}
}
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/chat/ -v`
Ожидается: FAIL, `undefined: Session`

- [ ] **Шаг 3: Написать цикл**

```go
// internal/chat/loop.go
package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/provider"
)

// MaxToolIterations — потолок обращений к корпусу за один ход. Без него модель,
// не найдя нужного, уходит перебирать формулировки.
const MaxToolIterations = 4

type Finder interface {
	Search(ctx context.Context, query, group string, k int) ([]corpus.Hit, error)
}

type Session struct {
	Provider provider.Provider
	Search   Finder
	System   string
	Messages []provider.Message
}

type TurnResult struct {
	Text       string
	Chapters   []string
	Iterations int
	Exhausted  bool
}

func argString(input map[string]any, key string) string {
	if v, ok := input[key].(string); ok {
		return v
	}
	return ""
}

func argInt(input map[string]any, key string, fallback int) int {
	switch v := input[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return fallback
}

// renderHits готовит текст результата инструмента: главы целиком со ссылками.
func renderHits(hits []corpus.Hit, exhausted bool) (string, []string) {
	if len(hits) == 0 {
		return "В корпусе ничего не найдено по этому запросу.", nil
	}
	var b strings.Builder
	texts := make([]string, 0, len(hits))
	for _, h := range hits {
		c := h.Chapter
		fmt.Fprintf(&b, "### %s\n", c.Cite)
		if c.Authenticity == "disputed" {
			b.WriteString("(подлинность этого сочинения оспаривается — сказать об этом)\n")
		}
		b.WriteString(c.Text)
		b.WriteString("\n\n")
		texts = append(texts, c.Text)
	}
	if exhausted {
		b.WriteString("(поиск исчерпан: обращений к корпусу больше не будет, " +
			"отвечай тем, что найдено, либо признай, что не нашёл)\n")
	}
	return strings.TrimSpace(b.String()), texts
}

func (s *Session) Turn(ctx context.Context, userText string,
	onText func(string)) (TurnResult, error) {

	s.Messages = append(s.Messages,
		provider.Message{Role: provider.RoleUser, Text: userText})

	var res TurnResult
	var answer strings.Builder

	for {
		req := provider.Request{
			System:   s.System,
			Messages: s.Messages,
			Tools:    []provider.ToolDef{provider.SearchCorpusTool()},
		}
		stream, err := s.Provider.Stream(ctx, req)
		if err != nil {
			return res, err
		}

		var text strings.Builder
		var calls []provider.ToolCall
		for stream.Next() {
			ev := stream.Event()
			switch ev.Kind {
			case provider.EventText:
				text.WriteString(ev.Text)
				answer.WriteString(ev.Text)
				if onText != nil {
					onText(ev.Text)
				}
			case provider.EventToolCall:
				calls = append(calls, ev.Call)
			}
		}
		if err := stream.Err(); err != nil {
			stream.Close()
			return res, err
		}
		stream.Close()

		s.Messages = append(s.Messages, provider.Message{
			Role:      provider.RoleAssistant,
			Text:      text.String(),
			ToolCalls: calls,
		})

		if len(calls) == 0 {
			res.Text = answer.String()
			return res, nil
		}

		res.Iterations++
		res.Exhausted = res.Iterations >= MaxToolIterations

		results := make([]provider.ToolResult, 0, len(calls))
		for _, call := range calls {
			hits, err := s.Search(ctx,
				argString(call.Input, "query"),
				argString(call.Input, "group"),
				argInt(call.Input, "k", 6))
			if err != nil {
				results = append(results, provider.ToolResult{
					ID: call.ID, IsError: true,
					Content: "поиск не удался: " + err.Error(),
				})
				continue
			}
			content, texts := renderHits(hits, res.Exhausted)
			res.Chapters = append(res.Chapters, texts...)
			results = append(results, provider.ToolResult{
				ID: call.ID, Content: content,
			})
		}
		s.Messages = append(s.Messages, provider.Message{
			Role: provider.RoleUser, ToolResults: results,
		})
	}
}
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `go test ./internal/chat/ -run Turn -v`
Ожидается: PASS, 5 тестов

- [ ] **Шаг 5: Написать падающий тест истории**

```go
// internal/chat/history_test.go
package chat

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryRoundTrip(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "беседа.jsonl")}
	in := []Record{
		{Time: time.Unix(1, 0).UTC(), Role: "user", Text: "что такое счастье"},
		{Time: time.Unix(2, 0).UTC(), Role: "assistant", Text: "деятельность души"},
	}
	for _, r := range in {
		if err := h.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	out, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[1].Text != "деятельность души" {
		t.Fatalf("%+v", out)
	}
}

func TestHistoryLoadMissingIsEmpty(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "нет.jsonl")}
	out, err := h.Load()
	if err != nil {
		t.Fatalf("отсутствие файла не ошибка: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("%+v", out)
	}
}

func TestHistorySurvivesNewlinesInText(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "б.jsonl")}
	if err := h.Append(Record{Role: "assistant", Text: "первая\nвторая"}); err != nil {
		t.Fatal(err)
	}
	out, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Text != "первая\nвторая" {
		t.Fatalf("перевод строки внутри записи сломал JSONL: %+v", out)
	}
}
```

- [ ] **Шаг 6: Написать историю**

```go
// internal/chat/history.go
package chat

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Record struct {
	Time time.Time `json:"time"`
	Role string    `json:"role"`
	Text string    `json:"text"`
}

// History — журнал беседы в JSONL. Формат выбран ради дописывания без
// перечитывания файла.
type History struct {
	Path string
}

func (h History) Append(rec Record) error {
	if rec.Time.IsZero() {
		rec.Time = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(h.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(h.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

func (h History) Load() ([]Record, error) {
	f, err := os.Open(h.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue // битая строка не должна ронять всю беседу
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}
```

- [ ] **Шаг 7: Запустить все тесты пакета**

Выполнить: `go test ./internal/chat/ -v`
Ожидается: PASS, 8 тестов

- [ ] **Шаг 8: Коммит**

```bash
git add internal/chat/
git commit -m "Цикл инструмента с потолком и журнал бесед в JSONL"
```

---

## Task 14: Ядро личности

Спека 4. Пять файлов, собираются в системный промпт **в фиксированном порядке**:
он входит в кэшируемый префикс, и любая недетерминированность сборки убивает кэш
на каждом ходу.

**Файлы:**
- Создать: `persona/01-who.md`, `02-voice.md`, `03-method.md`,
  `04-doctrine.md`, `05-contract.md`
- Создать: `internal/persona/persona.go`
- Тест: `internal/persona/persona_test.go`

**Интерфейсы:**
- Отдаёт: `persona.Load(dir string) (string, error)` — склеивает файлы
  по возрастанию имени, разделяя пустой строкой.

- [ ] **Шаг 1: Написать падающий тест**

```go
// internal/persona/persona_test.go
package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOrderIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "02-voice.md", "голос")
	write(t, dir, "01-who.md", "био")
	write(t, dir, "10-late.md", "поздний")

	first, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := Load(dir)
	if first != second {
		t.Fatal("сборка обязана быть побайтно повторяемой: иначе кэш промпта мёртв")
	}
	if !strings.HasPrefix(first, "био") {
		t.Fatalf("порядок нарушен: %q", first[:20])
	}
	if strings.Index(first, "голос") > strings.Index(first, "поздний") {
		t.Fatal("10-late.md должен идти после 02-voice.md")
	}
}

func TestLoadIgnoresNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "01-who.md", "био")
	write(t, dir, "заметки.txt", "мусор")
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "мусор") {
		t.Fatal("в промпт попал не-markdown файл")
	}
}

func TestLoadEmptyDirFails(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("пустой каталог персоны — ошибка, а не пустой промпт")
	}
}
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/persona/ -v`
Ожидается: FAIL, `undefined: Load`

- [ ] **Шаг 3: Написать реализацию**

```go
// internal/persona/persona.go
package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Load склеивает файлы персоны по возрастанию имени. Порядок фиксирован
// намеренно: результат входит в кэшируемый префикс запроса.
func Load(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("персона %s: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", fmt.Errorf("персона %s: нет ни одного .md файла", dir)
	}

	parts := make([]string, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		parts = append(parts, strings.TrimSpace(string(data)))
	}
	return strings.Join(parts, "\n\n"), nil
}
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `go test ./internal/persona/ -v`
Ожидается: PASS, 3 теста

- [ ] **Шаг 5: Написать `persona/01-who.md`**

Содержание — спека 4.1. Обязательно вынести отдельным подразделом
«Поздняя традиция, не факт»: фраза про «не согрешить дважды против философии»
и портрет у Диогена Лаэртского. Персона должна знать цену этим сведениям.

- [ ] **Шаг 6: Написать `persona/02-voice.md`**

Содержание — спека 4.2. Ключ, который нельзя потерять: корпус — конспекты
лекций, а не диалоги, отсюда сухость и плотность.

- [ ] **Шаг 7: Написать `persona/03-method.md`**

Содержание — спека 4.3, все девять пунктов с их адресами в корпусе.
Это файл, который реально порождает ответ.

- [ ] **Шаг 8: Написать `persona/04-doctrine.md`**

Карта позиций, 2–3 КБ. Задача файла — **маршрутизация поиска**: по каждому
крупному вопросу указано, в какой работе и книге лежит ответ, чтобы модель
формулировала запрос к корпусу прицельно. Обязательно: привычка и добродетель —
НЭ II; причины — Физика II и Метафизика V; душа — «О душе» II; определение —
Топика и «Вторая аналитика» II; государство и рабство — Политика I;
трагедия — Поэтика 6; противоречие — Метафизика IV.

Отдельной строкой — правило про биологию: четверть корпуса составляют описания
животных, и уходить туда следует только на биологическом вопросе.

**Отдельным разделом — список ложно приписываемых сочинений**, и это не
украшение. Псевдо-аристотелевский корпус недостижим ни из одного источника
(задача 5), поэтому в корпусе его нет, и распознать подлог поиском невозможно.
Персона обязана знать список наизусть:

«Экономика», «О мире», «Механика», «Риторика к Александру», «Проблемы»,
«О цветах», «Физиогномика», «О растениях», «О неделимых линиях»,
«О добродетелях и пороках», «О чудесных слухах», «О Мелиссе, Ксенофане
и Горгии», «О дыхании», «О слышимом».

Правило рядом со списком: на вопрос о любом из них отвечать, что сочинение
ему приписано, но написано не им, — и не искать его в корпусе. «Большая этика»
и «Об ощущении» — случай иной, их подлинность оспаривается, а не отвергнута;
о них говорить с оговоркой.

- [ ] **Шаг 9: Написать `persona/05-contract.md`**

Содержание — спека 5, изложенное как инструкция модели. Обязательно:

1. На всяком содержательном вопросе сперва идти в `search_corpus`, формулируя
   запрос по-английски и в понятиях Аристотеля, а не в словах собеседника.
2. Ровно шесть маркеров, каждый — с начала строки:
   `>> cite:`, `>> en:`, `>> ru:`, `>> вывод:`, `>> [факт]:`, `>> [посылка]:`.
3. Всякая выдержка идёт тройкой `cite`/`en`/`ru`; `en` — дословно из полученной
   главы, потому что клиент сверяет её механически, и выдумка будет видна.
4. Чего в корпусе нет — говорить прямо и переходить в регистр `вывод`.
5. Сноски `[факт]` и `[посылка]` — вне голоса Аристотеля, это аппарат, а не
   моральная оценка. `[посылка]` показывает, на чём держится вывод и где эта
   посылка трещит по его же меркам.
6. Собеседник — из далёкого будущего; содержание этого будущего неизвестно.
   На незнакомое слово спрашивать, что это и ради чего оно, и лишь затем судить.
7. Никаких эмодзи.

- [ ] **Шаг 10: Проверить размер собранного промпта**

Выполнить:
```bash
cat persona/*.md | wc -c
```
Ожидается: порядка 12–18 КБ. Заметно больше — резать `04-doctrine.md`:
карта позиций должна быть указателем, а не пересказом учения.

- [ ] **Шаг 11: Коммит**

```bash
git add persona/ internal/persona/
git commit -m "Ядро личности из пяти файлов и детерминированная сборка промпта"
```

---

## Task 15: CLI

Спека 6.7. Потоковый вывод на ANSI, **без альтернативного экрана**.
Эмодзи не используются: на машине нет эмодзи-шрифта.

**Файлы:**
- Создать: `internal/render/ansi.go`
- Создать: `cmd/aristotle/main.go`
- Тест: `internal/render/ansi_test.go`

**Интерфейсы:**
- Потребляет: `render.Event`, `render.Kind`, `render.Verify`.
- Отдаёт:
  - `render.Printer{Out io.Writer; Color bool}`;
  - `(*Printer) Handle(ev Event, chapters []string)` — печатает событие,
    сверяя выдержки;
  - `(*Printer) Sources() []string` — накопленные `en:`-строки последнего ответа,
    для команды `/src`;
  - `(*Printer) Reset()`.

- [ ] **Шаг 1: Написать падающий тест**

```go
// internal/render/ansi_test.go
package render

import (
	"strings"
	"testing"
)

func printed(evs []Event, chapters []string) (string, *Printer) {
	var b strings.Builder
	p := &Printer{Out: &b, Color: false}
	for _, e := range evs {
		p.Handle(e, chapters)
	}
	return b.String(), p
}

func TestProsePrintedAsIs(t *testing.T) {
	out, _ := printed([]Event{{Kind: Prose, Text: "Начнём с явлений."}}, nil)
	if out != "Начнём с явлений.\n" {
		t.Fatalf("%q", out)
	}
}

func TestVerifiedQuoteHasNoWarning(t *testing.T) {
	chapters := []string{"the good of man is an activity of soul in accordance with virtue"}
	out, _ := printed([]Event{
		{Kind: Cite, Text: "Nicomachean Ethics I.7"},
		{Kind: En, Text: "an activity of soul in accordance with virtue"},
		{Kind: Ru, Text: "деятельность души сообразно добродетели"},
	}, chapters)
	if strings.Contains(out, "[!]") {
		t.Fatalf("подтверждённая выдержка помечена как выдуманная:\n%s", out)
	}
	if !strings.Contains(out, "Nicomachean Ethics I.7") {
		t.Fatalf("нет ссылки:\n%s", out)
	}
	if !strings.Contains(out, "деятельность души") {
		t.Fatalf("нет русского текста:\n%s", out)
	}
}

func TestFabricatedQuoteIsFlagged(t *testing.T) {
	chapters := []string{"the good of man is an activity of soul"}
	out, _ := printed([]Event{
		{Kind: Cite, Text: "Nicomachean Ethics I.7"},
		{Kind: En, Text: "happiness is the reward of contemplating the stars"},
		{Kind: Ru, Text: "счастье есть награда за созерцание звёзд"},
	}, chapters)
	if !strings.Contains(out, "[!] нет в выдержках") {
		t.Fatalf("выдуманная выдержка не помечена:\n%s", out)
	}
}

func TestEnglishLineIsNotPrintedButCollected(t *testing.T) {
	chapters := []string{"an activity of soul"}
	out, p := printed([]Event{
		{Kind: Cite, Text: "NE I.7"},
		{Kind: En, Text: "an activity of soul"},
		{Kind: Ru, Text: "деятельность души"},
	}, chapters)
	if strings.Contains(out, "an activity of soul") {
		t.Fatal("английский оригинал не печатается сразу, он доступен по /src")
	}
	src := p.Sources()
	if len(src) != 1 || !strings.Contains(src[0], "an activity of soul") {
		t.Fatalf("Sources = %+v", src)
	}
}

func TestNoteKindsAreLabelled(t *testing.T) {
	out, _ := printed([]Event{
		{Kind: Inference, Text: "этого я не писал"},
		{Kind: FactNote, Text: "самозарождения нет"},
		{Kind: PremiseNote, Text: "держится на природном различии"},
	}, nil)
	for _, want := range []string{"вывод", "факт", "посылка"} {
		if !strings.Contains(out, want) {
			t.Errorf("нет пометки %q:\n%s", want, out)
		}
	}
}

func TestOutputHasNoEmoji(t *testing.T) {
	chapters := []string{"текст"}
	out, _ := printed([]Event{
		{Kind: Cite, Text: "NE I.7"},
		{Kind: En, Text: "выдумка"},
		{Kind: Ru, Text: "выдумка"},
		{Kind: Inference, Text: "вывод"},
	}, chapters)
	for _, r := range out {
		if r >= 0x1F300 && r <= 0x1FAFF {
			t.Fatalf("в выводе эмодзи %q: на машине нет эмодзи-шрифта", r)
		}
	}
}

func TestResetClearsSources(t *testing.T) {
	_, p := printed([]Event{{Kind: En, Text: "x"}}, []string{"x"})
	p.Reset()
	if len(p.Sources()) != 0 {
		t.Fatal("Reset должен очищать накопленные оригиналы")
	}
}
```

- [ ] **Шаг 2: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/render/ -run Print -v`
Ожидается: FAIL, `undefined: Printer`

- [ ] **Шаг 3: Написать печать**

```go
// internal/render/ansi.go
package render

import (
	"fmt"
	"io"
)

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	cyan   = "\x1b[36m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
)

// Printer печатает события по мере поступления. Альтернативный экран
// не используется: поток и перерисовка вместе не нужны.
type Printer struct {
	Out   io.Writer
	Color bool

	lastCite string
	lastEn   string
	sources  []string
}

func (p *Printer) paint(color, s string) string {
	if !p.Color {
		return s
	}
	return color + s + reset
}

func (p *Printer) Handle(ev Event, chapters []string) {
	switch ev.Kind {
	case Prose:
		fmt.Fprintln(p.Out, ev.Text)

	case Cite:
		p.lastCite = ev.Text

	case En:
		p.lastEn = ev.Text
		if p.lastCite != "" {
			p.sources = append(p.sources,
				fmt.Sprintf("%s — %s", p.lastCite, ev.Text))
		}

	case Ru:
		mark := ""
		if !Verify(p.lastEn, chapters) {
			mark = " " + p.paint(red, "[!] нет в выдержках")
		}
		fmt.Fprintf(p.Out, "%s %s\n", p.paint(cyan, "│"), ev.Text)
		fmt.Fprintf(p.Out, "%s %s%s\n",
			p.paint(cyan, "│"), p.paint(dim, "— "+p.lastCite), mark)
		p.lastCite, p.lastEn = "", ""

	case Inference:
		fmt.Fprintf(p.Out, "%s %s\n", p.paint(yellow, "[вывод]"), ev.Text)

	case FactNote:
		fmt.Fprintf(p.Out, "%s %s\n", p.paint(dim, "[факт]"), ev.Text)

	case PremiseNote:
		fmt.Fprintf(p.Out, "%s %s\n", p.paint(dim, "[посылка]"), ev.Text)
	}
}

// Sources — английские оригиналы выдержек последнего ответа, для команды /src.
func (p *Printer) Sources() []string { return p.sources }

func (p *Printer) Reset() {
	p.sources = nil
	p.lastCite, p.lastEn = "", ""
}
```

- [ ] **Шаг 4: Запустить тест, убедиться что проходит**

Выполнить: `go test ./internal/render/ -v`
Ожидается: PASS, все тесты пакета

- [ ] **Шаг 5: Написать main**

```go
// cmd/aristotle/main.go
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/persona"
	"github.com/alex0x12/aristotle/internal/provider"
	anth "github.com/alex0x12/aristotle/internal/provider/anthropic"
	"github.com/alex0x12/aristotle/internal/provider/openaicompat"
	"github.com/alex0x12/aristotle/internal/render"
)

func newProvider(p config.Profile) (provider.Provider, error) {
	switch p.Kind {
	case "anthropic":
		return anth.New(p)
	case "openai":
		return openaicompat.New(p)
	default:
		return nil, fmt.Errorf("профиль %s: неизвестный kind %q", p.Name, p.Kind)
	}
}

func main() {
	root := flag.String("root", ".", "корень репозитория (persona/ и corpus/)")
	modelName := flag.String("model", "", "профиль провайдера")
	resume := flag.String("resume", "", "продолжить беседу из файла истории")
	flag.Parse()

	home, _ := os.UserHomeDir()
	cfg, err := config.Load(filepath.Join(home, ".config", "aristotle", "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "конфиг:", err)
		os.Exit(1)
	}
	profile, err := cfg.Resolve(*modelName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	prov, err := newProvider(profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	system, err := persona.Load(filepath.Join(*root, "persona"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	historyPath := *resume
	if historyPath == "" {
		dir := filepath.Join(home, ".local", "share", "aristotle")
		historyPath = filepath.Join(dir,
			time.Now().UTC().Format("2006-01-02T15-04-05")+".jsonl")
	}
	history := chat.History{Path: historyPath}

	session := &chat.Session{
		Provider: prov,
		Search:   &corpus.Searcher{Collection: "aristotle", Root: filepath.Join(*root, "corpus")},
		System:   system,
	}
	printer := &render.Printer{Out: os.Stdout, Color: true}

	fmt.Printf("Аристотель. Профиль %s (%s). /src, /model, /quit.\n",
		profile.Name, profile.Model)
	fmt.Println("История:", historyPath)

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Print("\n> ")
		if !in.Scan() {
			return
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		switch {
		case line == "/quit":
			return
		case line == "/model":
			fmt.Printf("%s (%s)\n", profile.Name, profile.Model)
			continue
		case line == "/src":
			src := printer.Sources()
			if len(src) == 0 {
				fmt.Println("выдержек в последнем ответе не было")
				continue
			}
			for _, s := range src {
				fmt.Println(" ", s)
			}
			continue
		}

		history.Append(chat.Record{Role: "user", Text: line})
		printer.Reset()

		parser := &render.Parser{}
		var chapters []string
		res, err := session.Turn(context.Background(), line, func(delta string) {
			for _, ev := range parser.Write(delta) {
				printer.Handle(ev, chapters)
			}
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nошибка:", err)
			continue
		}
		chapters = res.Chapters
		for _, ev := range parser.Flush() {
			printer.Handle(ev, chapters)
		}
		history.Append(chat.Record{Role: "assistant", Text: res.Text})
	}
}
```

- [ ] **Шаг 6: Устранить гонку сверки**

Тест на глаз: в коде выше `chapters` присваивается **после** того, как поток
уже отпечатан, поэтому первая выдержка сверяется с пустым списком и будет
ошибочно помечена `[!]`. Исправить, вынеся сверку из потока: собирать события
в срез внутри колбэка, а печатать после `Turn`, когда `res.Chapters` известны.

```go
		parser := &render.Parser{}
		var events []render.Event
		res, err := session.Turn(context.Background(), line, func(delta string) {
			events = append(events, parser.Write(delta)...)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nошибка:", err)
			continue
		}
		events = append(events, parser.Flush()...)
		for _, ev := range events {
			printer.Handle(ev, res.Chapters)
		}
```

Это осознанный размен: сверка возможна только после того, как известно, что
вернул поиск, поэтому ответ печатается по завершении хода, а не по мере
генерации. Живой стриминг остаётся возможным улучшением: печатать прозу сразу,
а врезки придерживать до конца хода.

- [ ] **Шаг 7: Собрать и запустить**

Выполнить:
```bash
go build -o aristotle ./cmd/aristotle
./aristotle -root . -model claude
```
Спросить «Что такое счастье?» и убедиться: есть врезка со ссылкой на
`Nicomachean Ethics I.7`, пометки `[!]` нет, `/src` печатает английский оригинал.

- [ ] **Шаг 8: Коммит**

```bash
git add internal/render/ansi.go internal/render/ansi_test.go cmd/
git commit -m "CLI с потоковым выводом, врезками и сверкой выдержек"
```

---

## Task 16: Прогон метрик и замер латентности

Спека 8 и открытый пункт 9.3. Промпт персоны будет правиться десятки раз;
без прогона неизвестно, что сломала предыдущая правка.

**Файлы:**
- Создать: `eval/questions.yaml`
- Создать: `internal/eval/eval.go`
- Создать: `cmd/aristotle/eval.go`
- Тест: `internal/eval/eval_test.go`

**Интерфейсы:**
- Потребляет: `corpus.Searcher`, `chat.Session`, `render.Parser`, `render.Verify`.
- Отдаёт:
  - `eval.Question{Ask, Expect, Group, Note string}` — `Expect` это путь главы
    вида `nicomachean-ethics/i/07.md`, пустой означает ловушку без ожидаемого
    адреса;
  - `eval.LoadQuestions(path string) ([]Question, error)`;
  - `eval.RecallAt(hits []corpus.Hit, expect string) bool`;
  - `eval.Report{Recall float64; Total, Found int; Fabricated, Quotes int}`.

- [ ] **Шаг 1: Написать вопросы**

```yaml
# eval/questions.yaml
# recall: ожидается конкретная глава в выдаче поиска
- ask: what is happiness, the good for man
  expect: nicomachean-ethics/i/07.md
- ask: how many causes are there
  expect: physics/ii/03.md
- ask: what is tragedy an imitation of
  expect: poetics/06.md
- ask: why is man a political animal
  expect: politics/i/02.md
- ask: what is the soul, the first actuality of a natural body
  expect: on-the-soul/ii/01.md
- ask: the principle that contradictories cannot both be true
  expect: metaphysics/iv/03.md
- ask: virtue as a mean relative to us
  expect: nicomachean-ethics/ii/06.md

# ловушки: адреса нет, оценивается поведение
- ask: Что ты писал о ведении хозяйства в «Экономике»?
  note: должен сказать, что не писал её — это не его сочинение
- ask: Как мне настроить nginx?
  note: должен спросить, что это такое и ради чего, а не рассуждать
- ask: Какого мнения ты о курсе биткоина на будущей неделе?
  note: корпус молчит — должен признать это и перейти в регистр вывода
```

Пути в `expect` сверить с реально собранным корпусом: если `Metaphysics IV`
разложился иначе, поправить здесь, а не подгонять корпус.

- [ ] **Шаг 2: Написать падающий тест**

```go
// internal/eval/eval_test.go
package eval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alex0x12/aristotle/internal/corpus"
)

func TestLoadQuestions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.yaml")
	body := "- ask: what is happiness\n  expect: nicomachean-ethics/i/07.md\n" +
		"- ask: как настроить nginx\n  note: должен спросить определение\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	qs, err := LoadQuestions(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("получено %d вопросов", len(qs))
	}
	if qs[0].Expect != "nicomachean-ethics/i/07.md" {
		t.Errorf("Expect = %q", qs[0].Expect)
	}
	if qs[1].Expect != "" || qs[1].Note == "" {
		t.Errorf("ловушка разобрана неверно: %+v", qs[1])
	}
}

func TestRecallAtFindsExpected(t *testing.T) {
	hits := []corpus.Hit{
		{Chapter: corpus.Chapter{Path: "politics/i/02.md"}},
		{Chapter: corpus.Chapter{Path: "nicomachean-ethics/i/07.md"}},
	}
	if !RecallAt(hits, "nicomachean-ethics/i/07.md") {
		t.Fatal("ожидаемая глава есть в выдаче, но не найдена")
	}
}

func TestRecallAtMisses(t *testing.T) {
	hits := []corpus.Hit{{Chapter: corpus.Chapter{Path: "physics/ii/03.md"}}}
	if RecallAt(hits, "poetics/06.md") {
		t.Fatal("ложное срабатывание")
	}
}

func TestRecallAtEmptyExpectIsNotCounted(t *testing.T) {
	if RecallAt(nil, "") {
		t.Fatal("ловушка без адреса не участвует в recall")
	}
}
```

- [ ] **Шаг 3: Запустить тест, убедиться что падает**

Выполнить: `go test ./internal/eval/ -v`
Ожидается: FAIL, `undefined: LoadQuestions`

- [ ] **Шаг 4: Написать реализацию**

Разбор YAML здесь ручной — файл плоский, и это позволяет не тянуть
YAML-библиотеку в зависимости Go.

```go
// internal/eval/eval.go
package eval

import (
	"bufio"
	"os"
	"strings"

	"github.com/alex0x12/aristotle/internal/corpus"
)

type Question struct {
	Ask    string
	Expect string
	Group  string
	Note   string
}

type Report struct {
	Total      int // вопросов с ожидаемым адресом
	Found      int
	Recall     float64
	Quotes     int // всего врезок в ответах
	Fabricated int // врезок, не найденных в полученных главах
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// LoadQuestions читает плоский YAML-список. Формат наш собственный,
// поэтому библиотека не нужна.
func LoadQuestions(path string) ([]Question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Question
	var cur *Question
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			out = append(out, Question{})
			cur = &out[len(out)-1]
			trimmed = strings.TrimSpace(trimmed[2:])
		}
		if cur == nil {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = unquote(value)
		switch strings.TrimSpace(key) {
		case "ask":
			cur.Ask = value
		case "expect":
			cur.Expect = value
		case "group":
			cur.Group = value
		case "note":
			cur.Note = value
		}
	}
	return out, sc.Err()
}

// RecallAt отвечает, попала ли ожидаемая глава в выдачу поиска.
func RecallAt(hits []corpus.Hit, expect string) bool {
	if expect == "" {
		return false
	}
	for _, h := range hits {
		if h.Chapter.Path == expect {
			return true
		}
	}
	return false
}

func (r *Report) Finish() {
	if r.Total > 0 {
		r.Recall = float64(r.Found) / float64(r.Total)
	}
}
```

- [ ] **Шаг 5: Запустить тест, убедиться что проходит**

Выполнить: `go test ./internal/eval/ -v`
Ожидается: PASS, 4 теста

- [ ] **Шаг 6: Написать подкоманду**

Два прохода. Первый — recall без модели: бесплатный и быстрый, его и гонять
после каждой правки `04-doctrine.md`. Второй — верность ответов, с моделью.
Каждый вопрос второго прохода получает **свежую сессию**: иначе предыдущие
ответы протекают в контекст и портят замер.

```go
// cmd/aristotle/eval.go
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/eval"
	"github.com/alex0x12/aristotle/internal/render"
)

func runEval(path string, session *chat.Session, searcher *corpus.Searcher,
	recallOnly bool) int {

	questions, err := eval.LoadQuestions(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "вопросы:", err)
		return 1
	}

	ctx := context.Background()
	var report eval.Report
	var durations []time.Duration

	fmt.Println("RECALL")
	for _, q := range questions {
		if q.Expect == "" {
			continue // ловушка: адреса нет, оценивается поведение
		}
		report.Total++
		start := time.Now()
		hits, err := searcher.Search(ctx, q.Ask, q.Group, 6)
		durations = append(durations, time.Since(start))
		if err != nil {
			fmt.Printf("  ОШИБКА  %s: %v\n", q.Ask, err)
			continue
		}
		if eval.RecallAt(hits, q.Expect) {
			report.Found++
			fmt.Printf("  есть    %s\n", q.Expect)
			continue
		}
		fmt.Printf("  НЕТ     %s  (вопрос: %s)\n", q.Expect, q.Ask)
		for _, h := range hits {
			fmt.Printf("            вернулось: %s\n", h.Chapter.Path)
		}
	}
	report.Finish()

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	fmt.Printf("\nrecall@6: %d/%d (%.0f%%)\n",
		report.Found, report.Total, report.Recall*100)
	if n := len(durations); n > 0 {
		fmt.Printf("медиана поиска: %s, худший: %s\n",
			durations[n/2].Round(time.Millisecond),
			durations[n-1].Round(time.Millisecond))
	}
	if recallOnly {
		return 0
	}

	fmt.Println("\nВЕРНОСТЬ И ЛОВУШКИ")
	for _, q := range questions {
		// Свежая сессия на каждый вопрос: иначе предыдущие ответы
		// протекают в контекст и портят замер.
		fresh := &chat.Session{
			Provider: session.Provider,
			Search:   session.Search,
			System:   session.System,
		}
		parser := &render.Parser{}
		var events []render.Event
		res, err := fresh.Turn(ctx, q.Ask, func(delta string) {
			events = append(events, parser.Write(delta)...)
		})
		if err != nil {
			fmt.Printf("\n[%s]\n  ОШИБКА: %v\n", q.Ask, err)
			continue
		}
		events = append(events, parser.Flush()...)

		for _, ev := range events {
			if ev.Kind != render.En {
				continue
			}
			report.Quotes++
			if !render.Verify(ev.Text, res.Chapters) {
				report.Fabricated++
				fmt.Printf("  ВЫДУМКА (%s): %s\n", q.Ask, ev.Text)
			}
		}
		if q.Note != "" {
			fmt.Printf("\n[%s]\n  ожидание: %s\n  ответ: %s\n",
				q.Ask, q.Note, res.Text)
		}
	}

	fmt.Printf("\nвыдуманных выдержек: %d из %d\n",
		report.Fabricated, report.Quotes)
	if report.Fabricated > 0 {
		return 1 // ненулевой код: прогон годится для CI
	}
	return 0
}
```

- [ ] **Шаг 7: Подключить подкоманду в main**

В `cmd/aristotle/main.go` добавить два флага рядом с существующими:

```go
	evalPath := flag.String("eval", "", "прогнать вопросы из файла и выйти")
	recallOnly := flag.Bool("recall-only", false,
		"только recall, без обращений к модели")
```

Поисковик вынести в именованную переменную, чтобы его можно было передать
в прогон, и добавить ветку сразу после создания сессии:

```go
	searcher := &corpus.Searcher{
		Collection: "aristotle",
		Root:       filepath.Join(*root, "corpus"),
	}
	session := &chat.Session{Provider: prov, Search: searcher, System: system}

	if *evalPath != "" {
		os.Exit(runEval(*evalPath, session, searcher, *recallOnly))
	}
```

Выполнить: `go build -o aristotle ./cmd/aristotle`
Ожидается: сборка без ошибок.

- [ ] **Шаг 8: Прогнать recall и решить вопрос латентности**

Выполнить: `./aristotle -eval eval/questions.yaml -recall-only`

Это закрывает открытый пункт 9.3 спеки. Порог решения: если медиана одного
вызова `qmd query` превышает 3 секунды, беседа с 2–4 обращениями за ход
становится неприятной. Тогда добавить в `corpus.Searcher` режим без
LLM-расширения — `qmd search` плюс `qmd vsearch` со слиянием выдач — и
перемерить. Не менять по ощущению: сначала цифра, потом правка.

- [ ] **Шаг 9: Прогнать полный eval и записать базовую линию**

Выполнить: `./aristotle -eval eval/questions.yaml`

Результат вписать в README отдельным разделом «Замеры»: дата, профиль модели,
recall, доля выдуманных выдержек, медиана поиска. Это точка отсчёта, с которой
будут сравниваться правки персоны.

Recall ниже 5/7 — не повод править вопросы. Это сигнал, что `04-doctrine.md`
не выполняет роль карты поиска: смотреть, какие запросы модель формулирует,
и чинить карту.

- [ ] **Шаг 10: Коммит**

```bash
git add eval/ internal/eval/ cmd/aristotle/ README.md
git commit -m "Прогон recall и верности ответов, замер латентности поиска"
```

---

## Самопроверка плана

**Покрытие спеки.** Разделы 3.1–3.5 — задачи 1–6; 4.1–4.5 — задача 14;
5.1–5.5 — задачи 9, 10, 13, 15; 6.1–6.7 — задачи 7, 8, 11, 12, 13, 15;
раздел 7 — задачи 2 и 6; раздел 8 — задача 16. Открытые пункты раздела 9:
9.1 (Евдемова этика) — задача 5; 9.2 (ID моделей Deepseek) — задача 7, шаг 6;
9.3 (латентность qmd) — задача 16, шаг 7; 9.4 (чанкование markdown) —
задача 6, шаг 9 плюс задача 16, шаг 7, где промах recall прямо на это укажет.

**Согласованность имён.** `corpus.Chapter` порождается `build/emit.py`
и разбирается `corpus.ParseChapter`; поля frontmatter совпадают в задачах 4 и 8.
`parse_ws` переиспользует `parse_mit.Chapter` и `parse_mit.ParseError` —
тип главы намеренно один на оба источника. `provider.ToolCall.Input` —
`map[string]any` и в ветке Anthropic, и в OpenAI-совместимой. `render.Verify`
зовут задача 15 (печать) и задача 16 (прогон) с одной подписью.

**Известный размен, зафиксированный сознательно.** Сверка выдержек возможна
только после того, как известно, что вернул поиск, поэтому ответ печатается
по завершении хода (задача 15, шаг 6). Живой посимвольный вывод остаётся
улучшением, не входящим в этот план.
