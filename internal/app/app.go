// Package app собирает беседу одинаково для командной строки и
// веб-интерфейса: пути к файлам пользователя, профиль с провайдером,
// восстановление истории из журнала.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/provider"
	anth "github.com/alex0x12/aristotle/internal/provider/anthropic"
	"github.com/alex0x12/aristotle/internal/provider/openaicompat"
)

// Paths — где лежат файлы пользователя.
type Paths struct {
	Config  string // ~/.config/aristotle/config.json
	Keys    string // ~/.config/aristotle/keys.json
	History string // ~/.local/share/aristotle — журналы бесед
}

// DefaultPaths — пути в домашнем каталоге.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("домашний каталог: %w", err)
	}
	return Paths{
		Config:  filepath.Join(home, ".config", "aristotle", "config.json"),
		Keys:    filepath.Join(home, ".config", "aristotle", "keys.json"),
		History: filepath.Join(home, ".local", "share", "aristotle"),
	}, nil
}

// HistoryLayout — время начала беседы в имени журнала. Веб-интерфейс берёт
// имя файла без .jsonl за id беседы.
const HistoryLayout = "2006-01-02T15-04-05"

// NewHistoryPath — журнал новой беседы в каталоге dir; время — в UTC.
func NewHistoryPath(dir string, now time.Time) string {
	return filepath.Join(dir, now.UTC().Format(HistoryLayout)+".jsonl")
}

// NewSearcher — поиск по корпусу репозитория root.
func NewSearcher(root string) *corpus.Searcher {
	return &corpus.Searcher{Collection: "aristotle", Root: filepath.Join(root, "corpus")}
}

// DropLastTurn убирает последний ход — от последней реплики пользователя с
// текстом до конца. Результаты инструмента тоже идут с ролью user, но хода
// не начинают. Нужен «ещё раз».
func DropLastTurn(msgs []provider.Message) []provider.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser && len(msgs[i].ToolResults) == 0 {
			return msgs[:i]
		}
	}
	return msgs
}

// NewProvider создаёт провайдера по виду профиля.
func NewProvider(p config.Profile) (provider.Provider, error) {
	switch p.Kind {
	case "anthropic":
		return anth.New(p)
	case "openai":
		return openaicompat.New(p)
	default:
		return nil, fmt.Errorf("профиль %s: неизвестный kind %q", p.Name, p.Kind)
	}
}

// SwitchProfile разрешает имя профиля и создаёт для него провайдера. Не
// трогает ничего снаружи: при ошибке (нет такого профиля, не задан ключ)
// вызывающая сторона просто не подставляет возвращённые значения, и прежний
// профиль остаётся рабочим.
func SwitchProfile(cfg config.Config, name string) (config.Profile, provider.Provider, error) {
	p, err := cfg.Resolve(name)
	if err != nil {
		return config.Profile{}, nil, err
	}
	prov, err := NewProvider(p)
	if err != nil {
		return config.Profile{}, nil, err
	}
	return p, prov, nil
}

// ProfileNames — отсортированный список имён профилей, для подсказки в /model.
func ProfileNames(cfg config.Config) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResumeMessages превращает записи истории в сообщения провайдера. Роли,
// которых чат не понимает (не "user" и не "assistant" — например, битая
// запись с пустой ролью), пропускаются, а не роняют восстановление целиком.
// Запись ассистента с пустым текстом тоже пропускается: это след оборванного
// хода (ошибка провайдера, Ctrl-C посреди ответа), а не настоящая реплика —
// сообщение без содержимого провайдер не примет.
//
// История обрывается репликой пользователя после ЛЮБОЙ ошибки провайдера
// или Ctrl-C — то есть как раз в главном случае, ради которого возобновляют:
// вопрос записан, ответ — нет. Хвостовые записи с ролью user, оставшиеся без
// ответа ассистента, поэтому отбрасываются: иначе следующий ход дописывает
// вторую подряд реплику user, а API такое отвергает.
func ResumeMessages(records []chat.Record) []provider.Message {
	msgs := make([]provider.Message, 0, len(records))
	for _, rec := range records {
		var role provider.Role
		switch rec.Role {
		case "user":
			role = provider.RoleUser
		case "assistant":
			if rec.Text == "" {
				continue
			}
			role = provider.RoleAssistant
		default:
			continue
		}
		msgs = append(msgs, provider.Message{Role: role, Text: rec.Text})
	}
	for len(msgs) > 0 && msgs[len(msgs)-1].Role == provider.RoleUser {
		msgs = msgs[:len(msgs)-1]
	}
	return msgs
}

// LastUserMessage ищет последнюю реплику пользователя в истории сессии —
// для /again. Пустая строка означает, что повторять нечего.
//
// Результаты инструмента тоже несут Role: RoleUser (так chat.Session.Turn
// передаёт их провайдеру — см. internal/chat/loop.go) и идут в Messages
// вперемешку с настоящими репликами человека. У них Text всегда пуст и
// заполнен только ToolResults, поэтому такие записи обязаны пропускаться:
// иначе после хода с обращением к поиску /again находит пустую строку от
// последнего результата инструмента, а не настоящий последний вопрос.
func LastUserMessage(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role == provider.RoleUser && len(m.ToolResults) == 0 {
			return m.Text
		}
	}
	return ""
}
