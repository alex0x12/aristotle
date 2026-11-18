# Чат-агент «Аристотель».
#
# Быстрый старт:  make build && make run
# Всё остальное:  make help

BINARY  := aristotle
CMD     := ./cmd/aristotle
PY      := build/venv/bin/python
QUESTIONS := eval/questions.yaml

# Профиль провайдера: make run MODEL=openai
MODEL ?=
MODEL_FLAG := $(if $(MODEL),-model $(MODEL),)

.DEFAULT_GOAL := help
.PHONY: help build run web test test-go test-py check fmt vet eval eval-recall \
        corpus corpus-force index clean

help: ## Показать эту справку
	@printf 'Цели:\n'
	@grep -hE '^[a-z-]+:.*##' $(MAKEFILE_LIST) \
		| sed 's/:.*##/\t/' \
		| awk -F'\t' '{ printf "  %-14s %s\n", $$1, $$2 }'
	@printf '\nПеременные:\n'
	@printf '  %-14s %s\n' 'MODEL' 'профиль провайдера: deepseek (умолчание), openai, openrouter, claude'
	@printf '\nПримеры:\n'
	@printf '  make run\n'
	@printf '  make run MODEL=openrouter\n'
	@printf '  make eval-recall\n'

# --- сборка и запуск ---------------------------------------------------------

build: ## Собрать бинарник
	go build -o $(BINARY) $(CMD)

run: build ## Собрать и начать беседу
	./$(BINARY) $(MODEL_FLAG)

web: build ## Веб-интерфейс на 127.0.0.1:8337
	./$(BINARY) web

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
	./$(BINARY) -eval $(QUESTIONS) $(MODEL_FLAG)

# --- корпус ------------------------------------------------------------------
# Корпус уже собран и лежит в git. Пересобирать нужно, только если
# менялся build/works.yaml или парсеры. Подробности — в SOURCES.md.

corpus: ## Дособрать корпус, не трогая уже скачанное
	cd build && venv/bin/python build.py

corpus-force: ## Пересобрать корпус с нуля
	cd build && venv/bin/python build.py --force

index: ## Завести коллекцию qmd и построить эмбеддинги (разово, минуты)
	qmd collection add $(CURDIR)/corpus --name aristotle --pattern '**/*.md'
	qmd context add qmd://aristotle/ "Полный корпус Аристотеля по-английски, один файл на главу, с полями authenticity и group во frontmatter"
	qmd embed -c aristotle

# --- прочее ------------------------------------------------------------------

clean: ## Удалить бинарник
	rm -f $(BINARY)
