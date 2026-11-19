package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
)

// settingsDoc — документ настроек в ответах GET и PUT: адрес API, модель,
// усилие размышления, бюджет контекста и состояние ключа. Больше в настройках
// нет ничего.
type settingsDoc struct {
	BaseURL         string  `json:"base_url"`
	Model           string  `json:"model"`
	ReasoningEffort string  `json:"reasoning_effort"`
	ContextBudget   int     `json:"context_budget"`
	Key             keyView `json:"key"`
	// Degraded, Warning и Backup — только в ответе: config.json не читался
	// (испорчен или остался старого формата), и вместо него взяты умолчания;
	// предупреждение для экрана; имя копии испорченного файла.
	Degraded bool   `json:"degraded,omitempty"`
	Warning  string `json:"warning,omitempty"`
	Backup   string `json:"backup,omitempty"`
}

// settingsIn — тело PUT /api/settings: ровно четыре поля, все обязательные.
// Указатели нужны, чтобы пропущенное поле не превратилось молча в ноль: нулевой
// бюджет значит «без урезания».
type settingsIn struct {
	BaseURL         *string `json:"base_url"`
	Model           *string `json:"model"`
	ReasoningEffort *string `json:"reasoning_effort"`
	ContextBudget   *int    `json:"context_budget"`
}

// Пределы тел запросов. Ключ — до 4096 знаков (до 16 КиБ в UTF-8), настройки
// — четыре коротких поля.
const (
	maxSettingsBody = 16 << 10
	maxKeyBody      = 64 << 10
)

// decodeStrict читает тело запроса в v и отвергает неизвестные поля, лишнее
// после JSON и тело длиннее limit. Возвращает статус ответа и ошибку по-русски
// без содержимого тела: в теле может быть ключ. nameFields — называть ли в
// ошибке неизвестное поле: для настроек да, для ключа нет (в имени поля могла
// оказаться вставленная не туда строка ключа).
func decodeStrict(w http.ResponseWriter, r *http.Request, v any, limit int64, nameFields bool) (int, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	fail := func(err error) (int, error) {
		var ute *json.UnmarshalTypeError
		var se *json.SyntaxError
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			return http.StatusRequestEntityTooLarge, errors.New("тело запроса слишком велико")
		case errors.As(err, &ute):
			return http.StatusBadRequest, fmt.Errorf("поле %s: значение не того типа", ute.Field)
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			if !nameFields {
				return http.StatusBadRequest, errors.New("в запросе есть неизвестное поле")
			}
			return http.StatusBadRequest, fmt.Errorf("неизвестное поле %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
		case errors.As(err, &se), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return http.StatusBadRequest, errors.New("тело запроса — не JSON")
		default:
			return http.StatusBadRequest, errors.New("запрос не разобран")
		}
	}
	if err := dec.Decode(v); err != nil {
		return fail(err)
	}
	if _, err := dec.Token(); err != io.EOF {
		if err != nil {
			return fail(err)
		}
		return http.StatusBadRequest, errors.New("после JSON в запросе есть лишнее")
	}
	return 0, nil
}

// currentSettings — настройки как их видит экран; при испорченном config.json —
// умолчания с пометкой degraded и предупреждением.
func (s *Server) currentSettings() settingsDoc {
	cfg, degraded := s.loadConfig()
	doc := settingsDoc{BaseURL: cfg.BaseURL, Model: cfg.Model, ReasoningEffort: cfg.ReasoningEffort,
		ContextBudget: cfg.ContextBudget, Key: s.keyState()}
	if degraded {
		doc.Degraded, doc.Warning = true, configUnreadableText
	}
	return doc
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.currentSettings())
}

// settingsOf превращает присланное в конфиг, называя недостающее поле по имени.
func settingsOf(in settingsIn) (config.Config, error) {
	switch {
	case in.BaseURL == nil:
		return config.Config{}, errors.New("поле base_url обязательно")
	case in.Model == nil:
		return config.Config{}, errors.New("поле model обязательно")
	case in.ReasoningEffort == nil:
		return config.Config{}, errors.New("поле reasoning_effort обязательно")
	case in.ContextBudget == nil:
		return config.Config{}, errors.New("поле context_budget обязательно")
	}
	cfg := config.Config{BaseURL: *in.BaseURL, Model: *in.Model, ReasoningEffort: *in.ReasoningEffort,
		ContextBudget: *in.ContextBudget}.Normalized()
	return cfg, cfg.Validate()
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsIn
	if status, err := decodeStrict(w, r, &in, maxSettingsBody, true); err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	// Проверка — до любого обращения к диску: недопустимый документ не должен
	// ни создать копию, ни тронуть файл.
	cfg, err := settingsOf(in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	_, degraded := s.loadConfig()
	// Порядок: проверка документа (выше), копия, запись. Сохранение поверх
	// испорченного config.json (или оставшегося от версии с профилями) разрешено
	// — предупреждение обещает именно это, — но файл правили руками, и молча он
	// не уничтожается: сперва копия рядом. Нет копии — нет записи.
	backup := ""
	if degraded {
		var err error
		if backup, err = backupCorruptConfig(s.Paths.Config, s.utcNow()); err != nil {
			log.Printf("копия повреждённого config.json не сделана: %v", err)
			if errors.Is(err, errBackupExists) {
				http.Error(w, "копия повреждённого config.json с таким именем уже есть — повторите через секунду; настройки не записаны, файл не тронут",
					http.StatusConflict)
				return
			}
			http.Error(w, "не удалось сделать копию повреждённого config.json рядом с ним — настройки не записаны, файл не тронут",
				http.StatusInternalServerError)
			return
		}
	}
	if err := config.Save(s.Paths.Config, cfg); err != nil {
		log.Printf("настройки не записаны: %v", err)
		http.Error(w, "не удалось записать config.json", http.StatusInternalServerError)
		return
	}
	doc := s.currentSettings()
	doc.Backup = backup
	writeJSON(w, doc)
}

// keysUnreadable — отказ записи в испорченный keys.json; файл не тронут.
func (s *Server) keysUnreadable(w http.ResponseWriter, err error) {
	s.logKeysUnreadable(err) // в журнале сервера путь есть, в ответе — нет
	http.Error(w, keysUnreadableText+": ключ не изменён. Исправьте файл или удалите его вручную и повторите.",
		http.StatusConflict)
}

func (s *Server) handlePutKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key string `json:"key"`
	}
	if status, err := decodeStrict(w, r, &in, maxKeyBody, false); err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	key, err := keys.Normalize(in.Key) // до замка и до диска; ошибка ключа не повторяет
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Проверка прежнего файла и запись — одна операция: без замка два запроса
	// подряд прочли бы один и тот же файл. Испорченный файл Set не трогает.
	s.keysMu.Lock()
	defer s.keysMu.Unlock()
	if err := s.keyStore().Set(key); err != nil {
		var ue *keys.UnreadableError
		if errors.As(err, &ue) {
			s.keysUnreadable(w, err)
			return
		}
		log.Printf("ключ не записан: %v", err)
		http.Error(w, "не удалось записать keys.json", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "key": keyView{Set: true, Hint: keys.Hint(key)}})
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	s.keysMu.Lock()
	defer s.keysMu.Unlock()
	if err := s.keyStore().Delete(); err != nil {
		var ue *keys.UnreadableError
		if errors.As(err, &ue) {
			s.keysUnreadable(w, err)
			return
		}
		log.Printf("ключ не удалён: %v", err)
		http.Error(w, "не удалось записать keys.json", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

const defaultKeyCheckTimeout = 10 * time.Second

// checkFailText — причина, по которой проверка не дошла до ответа провайдера,
// по-русски и без адреса: в тексте ошибки net/http он есть, а показывать
// интерфейсу нужно «что случилось», не «куда».
func checkFailText(err error, timeout time.Duration) string {
	var dns *net.DNSError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("провайдер не ответил за %d с", int(timeout.Round(time.Second)/time.Second))
	case errors.Is(err, context.Canceled):
		return "проверка прервана"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "соединение отвергнуто: проверьте адрес API"
	case errors.As(err, &dns):
		return "имя адреса API не нашлось"
	default:
		return "не удалось связаться с провайдером: проверьте адрес API и сеть"
	}
}

// handleCheckKey проверяет сохранённый ключ по настроенному адресу бесплатным
// запросом списка моделей. Ответ всегда 200 с {ok, status?, message}; ни ключа,
// ни заголовков, ни тела ответа провайдера в нём нет.
func (s *Server) handleCheckKey(w http.ResponseWriter, r *http.Request) {
	fail := func(status int, msg string) {
		out := map[string]any{"ok": false, "message": msg}
		if status != 0 {
			out["status"] = status
		}
		writeJSON(w, out)
	}
	key, err := s.readKey()
	if err != nil {
		fail(0, keysUnreadableText) // без пути и без текста разбора
		return
	}
	if key == "" {
		fail(0, errNoKey.Error())
		return
	}
	cfg, _ := s.loadConfig() // испорченный config.json — умолчания
	timeout := s.keyCheckTimeout
	if timeout == 0 {
		timeout = defaultKeyCheckTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	code, err := app.CheckKey(ctx, cfg.BaseURL, key, s.httpClient())
	switch {
	case err != nil:
		log.Printf("проверка ключа: %v", err)
		fail(0, checkFailText(err, timeout))
	case code == http.StatusOK:
		writeJSON(w, map[string]any{"ok": true, "status": code, "message": "ключ принят"})
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		fail(code, fmt.Sprintf("ключ отклонён: %d %s", code, http.StatusText(code)))
	case code >= 300 && code < 400:
		fail(code, fmt.Sprintf("провайдер ответил переадресацией (%d): проверьте адрес API", code))
	default:
		fail(code, fmt.Sprintf("провайдер ответил %d %s: ключ не проверен", code, http.StatusText(code)))
	}
}
