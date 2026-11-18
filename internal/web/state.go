package web

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/alex0x12/aristotle/internal/keys"
)

// keyView — всё, что интерфейс знает о ключе. Самого ключа здесь нет: только
// есть ли он и его последние четыре знака.
type keyView struct {
	Set  bool   `json:"set"`
	Hint string `json:"hint,omitempty"`
	// Warning — почему ключ не удалось прочитать; пока только испорченный
	// keys.json (тогда Set = false). Ни пути, ни содержимого файла в нём нет.
	Warning string `json:"warning,omitempty"`
}

// keysUnreadableText — предупреждение вместо ключа, лежащего в испорченном
// keys.json, и начало отказа на запись в него.
const keysUnreadableText = keys.UnreadableText

// readKey — ключ из keys.json. Испорченный файл — *keys.UnreadableError; его
// причина (с путём, без содержимого) уходит в журнал сервера один раз.
func (s *Server) readKey() (string, error) {
	key, err := s.keyStore().Get()
	if err != nil {
		s.logKeysUnreadable(err)
		return "", err
	}
	s.cfgLogMu.Lock()
	s.lastKeysErr = ""
	s.cfgLogMu.Unlock()
	return key, nil
}

// keyState — состояние ключа для экрана. Испорченный keys.json не роняет
// ответ: экран настроек, где его чинят, должен открыться.
func (s *Server) keyState() keyView {
	key, err := s.readKey()
	var ue *keys.UnreadableError
	switch {
	case errors.As(err, &ue):
		return keyView{Warning: keysUnreadableText}
	case err != nil:
		return keyView{Warning: "ключ не прочитать"}
	case key == "":
		return keyView{}
	}
	return keyView{Set: true, Hint: keys.Hint(key)}
}

// monthTotal — итог месяца для шапки.
func monthTotal(dir string) usageTotal {
	// Журналы, которые не прочитались, итог не ломают: шапка не должна
	// пропадать из-за расхода.
	from, to := monthBounds(time.Now())
	_, month, err := aggregate(dir, from, to)
	if err != nil {
		log.Printf("расход за месяц: %v", err)
	}
	return month
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	cfg, degraded := s.loadConfig()
	out := map[string]any{
		"model": cfg.Model, "base_url": cfg.BaseURL, "context_budget": cfg.ContextBudget,
		"key": s.keyState(), "month": monthTotal(s.Paths.History),
	}
	if degraded { // у здорового конфига этих полей нет вовсе
		out["degraded"], out["warning"] = true, configUnreadableText
	}
	writeJSON(w, out)
}
