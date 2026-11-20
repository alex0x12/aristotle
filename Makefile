# Чат-агент «Аристотель».
#
# Быстрый старт:  make build && make run
# Всё остальное:  make help

BINARY  := aristotle
CMD     := ./cmd/aristotle
PREFIX  ?= $(HOME)/.local
PY      := build/venv/bin/python
QUESTIONS := eval/questions.yaml

# PHILOSOPHER=<slug> — пакет philosophers/<slug> вместо Аристотеля в корне
# (цели corpus, corpus-force, index, recall).
PHILOSOPHER ?=
ifeq ($(PHILOSOPHER),)
  COLLECTION  := aristotle
  CORPUS_DIR  := $(CURDIR)/corpus
  QMD_CONTEXT := Полный корпус Аристотеля по-английски, один файл на главу, с полями authenticity и group во frontmatter
  PHIL_FLAG   :=
else
  COLLECTION  := $(PHILOSOPHER)
  CORPUS_DIR  := $(CURDIR)/philosophers/$(PHILOSOPHER)/corpus
  QMD_CONTEXT := Корпус $(PHILOSOPHER) по-английски, один файл на главу, с полями authenticity и group во frontmatter
  PHIL_FLAG   := --philosopher $(PHILOSOPHER)
endif

.DEFAULT_GOAL := help
.PHONY: help build run web install uninstall test test-go test-py check fmt vet eval eval-recall \
        corpus corpus-force index index-ru recall assets install-skill uninstall-skill clean

help: ## Показать эту справку
	@printf 'Цели:\n'
	@grep -hE '^[a-z-]+:.*##' $(MAKEFILE_LIST) \
		| sed 's/:.*##/\t/' \
		| awk -F'\t' '{ printf "  %-14s %s\n", $$1, $$2 }'
	@printf '\nПримеры:\n'
	@printf '  make run\n'
	@printf '  make eval-recall\n'

# --- сборка и запуск ---------------------------------------------------------

build: ## Собрать бинарник
	go build -o $(BINARY) $(CMD)

run: build ## Собрать и начать беседу
	./$(BINARY)

web: build ## Веб-интерфейс на 127.0.0.1:8337
	./$(BINARY) web

# Бинарнику нужны persona/ и corpus/ этого репозитория: install вписывает в
# него путь сюда (значение -root по умолчанию), и `aristotle` работает из любого
# каталога. Переместили репозиторий — повторите make install (и make index).

install: ## Поставить aristotle в ~/.local/bin (другое место — PREFIX=…)
	@mkdir -p "$(PREFIX)/bin"
	go build -ldflags "-X 'main.defaultRoot=$(CURDIR)'" -o "$(PREFIX)/bin/$(BINARY)" $(CMD)
	@echo "установлен: $(PREFIX)/bin/$(BINARY) (репозиторий: $(CURDIR))"

uninstall: ## Убрать установленный aristotle
	rm -f "$(PREFIX)/bin/$(BINARY)"

# --- проверки ----------------------------------------------------------------

check: fmt vet test ## Форматирование, vet и все тесты

test: test-go test-py ## Все тесты: Go и Python

test-go: ## Тесты Go
	go test ./... -count=1

test-py: ## Тесты конвейера корпуса
	cd build && venv/bin/python -m pytest -q

fmt: ## Отформатировать Go-код
	gofmt -w .

vet: ## go vet
	go vet ./...

# --- замеры ------------------------------------------------------------------

eval-recall: build ## Recall поиска: быстро и без обращений к модели
	./$(BINARY) -eval $(QUESTIONS) -recall-only

eval: build ## Полный прогон: recall плюс верность ответов (тратит ключ)
	./$(BINARY) -eval $(QUESTIONS)

# --- корпус ------------------------------------------------------------------
# Корпус уже собран и лежит в git. Пересобирать нужно, только если
# менялся build/works.yaml или парсеры. Подробности — в SOURCES.md.

corpus: ## Дособрать корпус (PHILOSOPHER=<slug> — пакет), не трогая уже скачанное
	cd build && venv/bin/python build.py $(PHIL_FLAG)

corpus-force: ## Пересобрать корпус с нуля (PHILOSOPHER=<slug> — пакет)
	cd build && venv/bin/python build.py --force $(PHIL_FLAG)

index: ## Завести коллекцию qmd и построить эмбеддинги (PHILOSOPHER=<slug> — пакет; минуты)
	qmd collection add $(CORPUS_DIR) --name $(COLLECTION) --pattern '**/*.md'
	qmd context add qmd://$(COLLECTION)/ "$(QMD_CONTEXT)"
	qmd embed -c $(COLLECTION)

# Русский корпус — отдельной коллекцией: он локальный (corpus-ru/ в .gitignore)
# и не смешивается с английским при поиске.
RU_COLLECTION  := aristotle-ru
RU_CORPUS_DIR  := $(CURDIR)/corpus-ru
RU_QMD_CONTEXT := Полный русский корпус Аристотеля: переводы четырёхтомника «Философское наследие» (1976–1983) и работы вне его; один файл на главу, у «О частях животных» и «О возникновении животных» — файл на книгу; source: pdf/file, lang: ru

index-ru: ## Завести русскую коллекцию qmd и построить эмбеддинги (минуты)
	qmd collection show $(RU_COLLECTION) >/dev/null 2>&1 || \
		qmd collection add $(RU_CORPUS_DIR) --name $(RU_COLLECTION) --pattern '**/*.md'
	qmd context add qmd://$(RU_COLLECTION)/ "$(RU_QMD_CONTEXT)"
	qmd embed -c $(RU_COLLECTION)

recall: ## Recall поиска по вопросам, без модели (PHILOSOPHER=<slug> — пакет)
	cd build && venv/bin/python recall.py $(PHIL_FLAG)

# --- веб-интерфейс -----------------------------------------------------------
# Изображения и шрифты уже лежат в git (internal/web/static); качать заново
# нужно, только если меняется их набор.

assets: ## Скачать изображения и шрифты веб-интерфейса (результат уже в git)
	bash build/web-assets.sh

# --- скилл для Claude --------------------------------------------------------
# Скилл живёт в репозитории, а Claude ищет его в ~/.claude/skills: ставим
# ссылку, поэтому правка в репозитории действует сразу.

SKILL_SRC  := $(CURDIR)/skills/philosopher-agent
SKILL_LINK := $(HOME)/.claude/skills/philosopher-agent

install-skill: ## Поставить скилл «философ → чат-агент» ссылкой в ~/.claude/skills
	@mkdir -p "$(dir $(SKILL_LINK))"
	@ln -sfn "$(SKILL_SRC)" "$(SKILL_LINK)"
	@echo "скилл: $(SKILL_LINK) -> $(SKILL_SRC)"

uninstall-skill: ## Убрать ссылку на скилл (только свою)
	@if [ "$$(readlink "$(SKILL_LINK)")" = "$(SKILL_SRC)" ]; then \
	    rm -f "$(SKILL_LINK)" && echo "удалено: $(SKILL_LINK)"; \
	else \
	    echo "не наша ссылка или её нет: $(SKILL_LINK)"; \
	fi

# --- прочее ------------------------------------------------------------------

clean: ## Удалить бинарник
	rm -f $(BINARY)
