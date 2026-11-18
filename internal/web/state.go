package web

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
)

// keyView — всё, что интерфейс знает о ключе. Самого ключа здесь нет.
type keyView struct {
	Source string `json:"source"` // env, file, none
	Hint   string `json:"hint,omitempty"`
	// Warning — почему ключ не удалось прочитать; пока только испорченный
	// keys.json. Ни пути, ни содержимого файла в нём нет.
	Warning string `json:"warning,omitempty"`
}

// keysUnreadableText — предупреждение вместо профиля, чей ключ лежал бы в
// испорченном keys.json, и начало отказа на запись в него.
const keysUnreadableText = "keys.json не читается"

// keysUnreadableError — keys.json есть, но не разбирается (или не читается).
// Настоящая ошибка store.Get несёт путь к файлу и кусок его содержимого, поэтому
// Error() отдаёт только фиксированный текст — он и уходит в ответы; настоящая
// доступна через Unwrap для журнала сервера.
type keysUnreadableError struct{ cause error }

func (e *keysUnreadableError) Error() string { return keysUnreadableText }
func (e *keysUnreadableError) Unwrap() error { return e.cause }

// isKeysUnreadable — ошибка из resolve про испорченный keys.json, а не про
// профиль; сама ошибка (для журнала) вторым результатом.
func isKeysUnreadable(err error) (*keysUnreadableError, bool) {
	var ke *keysUnreadableError
	return ke, errors.As(err, &ke)
}

type profileView struct {
	Name            string         `json:"name"`
	Builtin         bool           `json:"builtin"`
	Kind            string         `json:"kind"`
	BaseURL         string         `json:"base_url"`
	Model           string         `json:"model"`
	KeyEnv          string         `json:"key_env"`
	Reasoning       bool           `json:"reasoning"`
	ReasoningEffort string         `json:"reasoning_effort"`
	Temperature     *float64       `json:"temperature"`
	MaxOutputTokens int            `json:"max_output_tokens"`
	ContextBudget   int            `json:"context_budget"`
	Prices          *config.Prices `json:"prices"`
	Key             keyView        `json:"key"`
}

func viewOf(p config.Profile) profileView {
	_, builtin := config.Builtin().Profiles[p.Name]
	v := profileView{
		Name: p.Name, Builtin: builtin, Kind: p.Kind, BaseURL: p.BaseURL, Model: p.Model,
		KeyEnv: p.KeyEnv, Reasoning: p.Reasoning, ReasoningEffort: p.ReasoningEffort,
		Temperature: p.Temperature, MaxOutputTokens: p.MaxOutputTokens,
		ContextBudget: p.ContextBudget, Prices: p.Prices,
	}
	switch app.KeySource(p) {
	case "env":
		// Resolve при заданной переменной файл не читает и p.Key оставляет
		// пустым: подсказка берётся из самой переменной.
		v.Key = keyView{Source: "env", Hint: keys.Hint(os.Getenv(p.KeyEnv))}
	case "file":
		v.Key = keyView{Source: "file", Hint: keys.Hint(p.Key)}
	default:
		v.Key = keyView{Source: "none"}
	}
	return v
}

// resolve — профиль с ключом из keys.json. Ошибку чтения файла заменяет на
// *keysUnreadableError; вместе с ней возвращается профиль без ключа, чтобы
// вызывающему было что показать. Профиль, которого нет или у которого неверный
// kind, — другая ошибка, она проходит как есть. В журнал resolve не пишет:
// профилей много, а причина одна, и вызывающий запишет её один раз.
func (s *Server) resolve(cfg config.Config, store keys.Store, name string) (config.Profile, error) {
	p, err := app.Resolve(cfg, store, name)
	if err == nil {
		return p, nil
	}
	raw, cerr := cfg.Resolve(name)
	if cerr != nil {
		raw = cfg.Profiles[name]
		raw.Name = name
		return raw, cerr
	}
	return raw, &keysUnreadableError{cause: err}
}

// profiles — все профили с ключами из keys.json, по имени. Испорченный
// keys.json не роняет ответ: экран настроек, где его чинят, должен открыться.
// Профили, чей ключ в окружении, показываются как обычно (файл им не нужен), а
// остальные — без ключа и с предупреждением. Так же не роняет ответ и испорченный
// config.json: тогда профили встроенные, а degraded = true (см. loadConfig).
func (s *Server) profiles() (cfg config.Config, views []profileView, degraded bool) {
	cfg, store, degraded := s.loadConfig()
	logged := false
	for _, name := range app.ProfileNames(cfg) {
		p, err := s.resolve(cfg, store, name)
		v := viewOf(p)
		if ke, ok := isKeysUnreadable(err); ok {
			v.Key = keyView{Source: "none", Warning: keysUnreadableText}
			if !logged { // в журнале сервера путь есть, в ответе — нет
				logged = true
				log.Printf("keys.json: %v", ke.cause)
			}
		}
		views = append(views, v)
	}
	return cfg, views, degraded
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	cfg, views, degraded := s.profiles()
	// Итог месяца для шапки. Журналы, которые не прочитались, итог не
	// ломают: шапка не должна терять профили из-за расхода.
	from, to := monthBounds(time.Now())
	_, month, err := aggregate(s.Paths.History, from, to)
	if err != nil {
		log.Printf("расход за месяц: %v", err)
	}
	out := map[string]any{"default": cfg.Default, "corpus_k": cfg.CorpusK, "profiles": views, "month": month}
	if degraded { // у здорового конфига этих полей нет вовсе
		out["degraded"], out["warning"] = true, configUnreadableText
	}
	writeJSON(w, out)
}
