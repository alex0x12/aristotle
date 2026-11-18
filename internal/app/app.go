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
	"github.com/alex0x12/aristotle/internal/keys"
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

// envKeySet — задана ли переменная окружения с ключом профиля.
func envKeySet(p config.Profile) bool {
	return p.KeyEnv != "" && os.Getenv(p.KeyEnv) != ""
}

// Resolve — профиль по имени с ключом из keys.json. Переменная окружения
// профиля важнее: когда она задана, файл не читается вовсе, чтобы испорченный
// keys.json не ломал запуск и /model тому, у кого ключ в окружении. Ошибка
// чтения файла доходит до вызывающего, только если окружение ключа не даёт.
func Resolve(cfg config.Config, store keys.Store, name string) (config.Profile, error) {
	p, err := cfg.Resolve(name)
	if err != nil {
		return config.Profile{}, err
	}
	if envKeySet(p) {
		return p, nil
	}
	key, err := store.Get(p.Name)
	if err != nil {
		return config.Profile{}, err
	}
	p.Key = key
	return p, nil
}

// KeySource — откуда возьмётся ключ профиля: "env", "file" или "none".
func KeySource(p config.Profile) string {
	switch {
	case envKeySet(p):
		return "env"
	case p.Key != "":
		return "file"
	default:
		return "none"
	}
}

// SwitchProfile разрешает имя профиля и создаёт для него провайдера. Не
// трогает ничего снаружи: при ошибке (нет такого профиля, не задан ключ)
// вызывающая сторона просто не подставляет возвращённые значения, и прежний
// профиль остаётся рабочим.
func SwitchProfile(cfg config.Config, store keys.Store, name string) (config.Profile, provider.Provider, error) {
	p, err := Resolve(cfg, store, name)
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

// AnswerKept — входит ли ответ из журнала в историю модели. chat.Turn
// откатывает неудавшийся или остановленный ход целиком (вопрос, начало ответа,
// обращения к поиску), а пустой ответ провайдер не принял бы сообщением. Такой
// ход остаётся в журнале и на экране, но не в истории для модели — ни в живой
// сессии, ни после восстановления из журнала. Записи без stop_reason (журналы
// командной строки) с текстом остаются: командная строка ошибочных ходов не пишет.
func AnswerKept(rec chat.Record) bool {
	return rec.Text != "" && rec.StopReason != "error" && rec.StopReason != "cancelled"
}

// Состояние хода журнала при восстановлении: вопрос ждёт ответа, пара стоит
// в истории, ход откатан и в истории его нет.
type turnState int

const (
	turnOpen turnState = iota
	turnKept
	turnGone
)

// replay проходит журнал так, как проживала его живая сессия: откатанные ходы
// не оставляют следа, а retract снимает последний ход журнала — и только если
// тот ход стоит в истории. Иначе отзыв после сбоя снял бы предыдущий, удавшийся
// ход. Второй результат — состояния ходов журнала, по одному на каждый вопрос
// (за вычетом отозванных), как их считает items.
func replay(records []chat.Record) ([]provider.Message, []turnState) {
	msgs := make([]provider.Message, 0, len(records))
	var turns []turnState
	for _, rec := range records {
		switch rec.Role {
		case "user":
			// Вопрос без ответа посреди журнала (процесс убит на ходу) в
			// истории не остаётся: иначе рядом встанут два user подряд.
			if n := len(turns); n > 0 && turns[n-1] == turnOpen {
				msgs = msgs[:len(msgs)-1]
				turns[n-1] = turnGone
			}
			msgs = append(msgs, provider.Message{Role: provider.RoleUser, Text: rec.Text})
			turns = append(turns, turnOpen)
		case "assistant":
			n := len(turns)
			if n == 0 || turns[n-1] != turnOpen {
				continue // ответ без вопроса: потерянная или битая запись
			}
			if !AnswerKept(rec) {
				msgs = msgs[:len(msgs)-1]
				turns[n-1] = turnGone
				continue
			}
			msgs = append(msgs, provider.Message{Role: provider.RoleAssistant, Text: rec.Text})
			turns[n-1] = turnKept
		case chat.RoleRetract:
			// «Ещё раз»: последний вопрос журнала и ответ на него отозваны.
			n := len(turns)
			if n == 0 {
				continue
			}
			switch turns[n-1] {
			case turnOpen:
				msgs = msgs[:len(msgs)-1]
			case turnKept:
				msgs = msgs[:len(msgs)-2]
			}
			turns = turns[:n-1]
		}
		// Остальные роли (meta, trim, битые) историю не меняют.
	}
	return msgs, turns
}

// ResumeMessages превращает записи истории в сообщения провайдера — ровно те,
// что были бы в живой сессии перед перезапуском: сообщения чередуются user и
// assistant. Роли, которых чат не понимает (не "user" и не "assistant" —
// например, битая запись с пустой ролью), пропускаются, а не роняют
// восстановление целиком. Ход, чей ответ не входит в историю (см. AnswerKept),
// выпадает целиком, вместе с вопросом: сообщение без содержимого провайдер не
// примет, а оставшийся без ответа вопрос дал бы два user подряд.
//
// История обрывается репликой пользователя после ЛЮБОЙ ошибки провайдера
// или Ctrl-C — то есть как раз в главном случае, ради которого возобновляют:
// вопрос записан, ответ — нет. Хвостовой вопрос без ответа поэтому
// отбрасывается: иначе следующий ход дописывает вторую подряд реплику user,
// а API такое отвергает.
//
// Запись retract отзывает последний вопрос с ответом — так журнал помнит «ещё раз».
func ResumeMessages(records []chat.Record) []provider.Message {
	msgs, turns := replay(records)
	if n := len(turns); n > 0 && turns[n-1] == turnOpen {
		msgs = msgs[:len(msgs)-1]
	}
	return msgs
}

// LastTurnKept — стоит ли последний ход журнала в истории модели. По этому
// ответу «ещё раз» решает, снимать ли ход из живой сессии: откатанного хода в
// ней нет, и снять предыдущий вместо него нельзя.
func LastTurnKept(records []chat.Record) bool {
	_, turns := replay(records)
	return len(turns) > 0 && turns[len(turns)-1] == turnKept
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
