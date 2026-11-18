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
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
)

// settingsDoc — документ настроек, один и тот же в GET и в PUT. В PUT поля
// builtin и key каждого профиля принимаются (интерфейс шлёт обратно то, что
// получил), но не читаются: ключи меняются только через /api/keys.
type settingsDoc struct {
	Default  string        `json:"default"`
	CorpusK  int           `json:"corpus_k"`
	Profiles []profileView `json:"profiles"`
	// Removed — имена профилей, которые пользователь убрал сам в этом сеансе
	// правки. Ключ из keys.json удаляется только у них (и только если профиля
	// нет в новых настройках); по одному отсутствию в документе — никогда.
	Removed []string `json:"removed,omitempty"`
	// Degraded, Warning и Backup — только в ответе (в PUT принимаются и не
	// читаются): config.json не читался, и вместо него взят встроенный
	// конфиг; предупреждение для экрана; имя копии испорченного файла.
	Degraded bool   `json:"degraded,omitempty"`
	Warning  string `json:"warning,omitempty"`
	Backup   string `json:"backup,omitempty"`
}

// Пределы значений. Верхние нужны не как мнение о «разумном», а чтобы
// опечатка (лишний нуль) не стала настройкой: огромная цена даёт в сумме
// +Inf, и JSON журнала уже не записать.
const (
	maxCorpusK    = 20        // глав на поиск; каждая приходит в запрос целиком
	maxOutputCap  = 1_000_000 // токенов ответа
	maxBudgetCap  = 10_000_000
	maxPriceCap   = 1_000_000.0 // долларов за миллион токенов
	maxProfiles   = 64
	maxModelRunes = 200
	maxURLBytes   = 500
	maxKeyBytes   = 4096
)

var (
	profileNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	keyEnvRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
)

// fieldErr — ошибка настройки с названием поля, по-русски.
func fieldErr(profile, field, format string, a ...any) error {
	why := fmt.Sprintf(format, a...)
	if profile == "" {
		return fmt.Errorf("%s: %s", field, why)
	}
	return fmt.Errorf("профиль %s, поле %s: %s", profile, field, why)
}

func hasSpaceOrControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// checkBaseURL — адрес API: только http и https, хост обязателен, логина и
// пароля в адресе нет (они попали бы в сообщения об ошибках).
func checkBaseURL(profile, raw string, required bool) error {
	if raw == "" {
		if required {
			return fieldErr(profile, "base_url", "у OpenAI-совместимого профиля адрес обязателен")
		}
		return nil
	}
	if len(raw) > maxURLBytes || hasSpaceOrControl(raw) {
		return fieldErr(profile, "base_url", "адрес без пробелов и управляющих знаков, до %d байт", maxURLBytes)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fieldErr(profile, "base_url", "адрес должен начинаться с http:// или https:// и называть хост")
	}
	if u.User != nil {
		return fieldErr(profile, "base_url", "логин и пароль в адресе не допускаются")
	}
	return nil
}

// checkProfile проверяет один присланный профиль. Правила про reasoning и
// temperature повторяют то, что провайдеры делают на самом деле: у рассуждающей
// модели OpenAI-совместимого API температуры нет (её там не отправят), а
// reasoning_effort уходит только ей; Anthropic усилия не знает вовсе.
// Лишнее поле не принимается молча — иначе настройка выглядела бы действующей.
func checkProfile(p profileView) error {
	n := p.Name
	switch {
	case !profileNameRe.MatchString(n):
		return fieldErr("", "name", "имя профиля %q: строчная латиница, цифры и дефис, до 32 знаков, начинается не с дефиса", n)
	case p.Kind != config.KindAnthropic && p.Kind != config.KindOpenAI:
		return fieldErr(n, "kind", "%q — ожидался anthropic или openai", p.Kind)
	}
	model := strings.TrimSpace(p.Model)
	if model == "" || len([]rune(model)) > maxModelRunes || strings.ContainsFunc(model, unicode.IsControl) {
		return fieldErr(n, "model", "название модели обязательно, без управляющих знаков, до %d знаков", maxModelRunes)
	}
	if err := checkBaseURL(n, strings.TrimSpace(p.BaseURL), p.Kind == config.KindOpenAI); err != nil {
		return err
	}
	if env := strings.TrimSpace(p.KeyEnv); env != "" && !keyEnvRe.MatchString(env) {
		return fieldErr(n, "key_env", "имя переменной окружения: латиница, цифры и «_», не с цифры")
	}
	if e := p.ReasoningEffort; e != "" {
		switch {
		case e != "low" && e != "medium" && e != "high":
			return fieldErr(n, "reasoning_effort", "%q — ожидалось low, medium или high", e)
		case p.Kind != config.KindOpenAI:
			return fieldErr(n, "reasoning_effort", "усилие рассуждения есть только у OpenAI-совместимых профилей")
		case !p.Reasoning:
			return fieldErr(n, "reasoning_effort", "усилие рассуждения задаётся только рассуждающей модели (reasoning)")
		}
	}
	if t := p.Temperature; t != nil {
		hi := 2.0 // OpenAI-совместимые API
		if p.Kind == config.KindAnthropic {
			hi = 1
		}
		switch {
		case p.Reasoning:
			return fieldErr(n, "temperature", "у рассуждающей модели температуры нет — уберите её или снимите reasoning")
		case *t < 0 || *t > hi:
			return fieldErr(n, "temperature", "от 0 до %g для этого типа профиля", hi)
		}
	}
	switch {
	case p.MaxOutputTokens < 0 || p.MaxOutputTokens > maxOutputCap:
		return fieldErr(n, "max_output_tokens", "от 0 до %d (0 — умолчание провайдера)", maxOutputCap)
	case p.ContextBudget < 0 || p.ContextBudget > maxBudgetCap:
		return fieldErr(n, "context_budget", "от 0 до %d (0 — без урезания)", maxBudgetCap)
	}
	if pr := p.Prices; pr != nil {
		for field, v := range map[string]float64{"input": pr.Input, "output": pr.Output, "cache_read": pr.CacheRead, "cache_write": pr.CacheWrite} {
			if v < 0 || v > maxPriceCap {
				return fieldErr(n, "prices."+field, "от 0 до %g долларов за миллион токенов", maxPriceCap)
			}
		}
	}
	return nil
}

// toConfig проверяет присланные настройки и превращает их в config.Config.
// Встроенный профиль, которого нет в списке, вернётся с умолчаниями:
// config.Save пишет только отличия, так что и прежние правки такого профиля
// сбрасываются. Профилем по умолчанию может быть и пропущенный встроенный.
func toConfig(in settingsDoc) (config.Config, error) {
	c := config.Config{Default: in.Default, CorpusK: in.CorpusK, Profiles: map[string]config.Profile{}}
	if in.CorpusK < 1 || in.CorpusK > maxCorpusK {
		return c, fieldErr("", "corpus_k", "глав на поиск — от 1 до %d", maxCorpusK)
	}
	if len(in.Profiles) == 0 || len(in.Profiles) > maxProfiles {
		return c, fieldErr("", "profiles", "нужен от 1 до %d профилей", maxProfiles)
	}
	for _, p := range in.Profiles {
		if err := checkProfile(p); err != nil {
			return c, err
		}
		if _, dup := c.Profiles[p.Name]; dup {
			return c, fieldErr("", "name", "профиль %q указан дважды", p.Name)
		}
		c.Profiles[p.Name] = config.Profile{
			Kind: p.Kind, BaseURL: strings.TrimSpace(p.BaseURL), Model: strings.TrimSpace(p.Model),
			KeyEnv: strings.TrimSpace(p.KeyEnv), Reasoning: p.Reasoning, ReasoningEffort: p.ReasoningEffort,
			Temperature: p.Temperature, MaxOutputTokens: p.MaxOutputTokens,
			ContextBudget: p.ContextBudget, Prices: p.Prices,
		}
	}
	_, listed := c.Profiles[c.Default]
	_, builtin := config.Builtin().Profiles[c.Default]
	if !listed && !builtin {
		return c, fieldErr("", "default", "профиля %q нет", c.Default)
	}
	return c, nil
}

// decodeStrict читает тело запроса в v и отвергает неизвестные поля и лишнее
// после JSON. Ошибки по-русски и без содержимого тела: в теле может быть ключ.
func decodeStrict(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var ute *json.UnmarshalTypeError
		var se *json.SyntaxError
		switch {
		case errors.As(err, &ute):
			return fmt.Errorf("поле %s: значение не того типа", ute.Field)
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return fmt.Errorf("неизвестное поле %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
		case errors.As(err, &se), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("тело запроса — не JSON")
		default:
			return errors.New("запрос не разобран")
		}
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("после JSON в запросе есть лишнее")
	}
	return nil
}

// currentSettings — настройки как их видит экран; при испорченном config.json —
// встроенные с пометкой degraded и предупреждением.
func (s *Server) currentSettings() settingsDoc {
	cfg, views, degraded := s.profiles()
	doc := settingsDoc{Default: cfg.Default, CorpusK: cfg.CorpusK, Profiles: views}
	if degraded {
		doc.Degraded, doc.Warning = true, configUnreadableText
	}
	return doc
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.currentSettings())
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsDoc
	if err := decodeStrict(r, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, err := toConfig(in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	old, store, degraded := s.loadConfig()
	if err := checkRemoved(in.Removed, old); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Порядок: проверка документа (выше), копия, запись. Сохранение поверх
	// испорченного config.json разрешено — предупреждение обещает именно это, — но
	// файл правили руками, и молча он не уничтожается: сперва копия рядом. Нет
	// копии — нет записи.
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
	s.dropKeys(store, in.Removed, cfg)
	doc := s.currentSettings()
	doc.Backup = backup
	writeJSON(w, doc)
}

// checkRemoved проверяет список убранных профилей до всякой записи: имя должно
// быть у сохранённого пользовательского профиля (встроенный убрать нельзя, он
// вернётся; несуществующий — признак устаревшей вкладки или опечатки) и не
// повторяться.
func checkRemoved(removed []string, old config.Config) error {
	if len(removed) > maxProfiles {
		return fieldErr("", "removed", "не больше %d имён", maxProfiles)
	}
	seen := map[string]bool{}
	for _, name := range removed {
		_, builtin := config.Builtin().Profiles[name]
		_, saved := old.Profiles[name]
		switch {
		case seen[name]:
			return fieldErr("", "removed", "имя %q указано дважды", name)
		case builtin:
			return fieldErr("", "removed", "профиль %q встроенный — убрать его нельзя, он возвращается с умолчаниями", name)
		case !saved:
			return fieldErr("", "removed", "профиля %q нет в сохранённых настройках (экран устарел? откройте его заново)", name)
		}
		seen[name] = true
	}
	return nil
}

// dropKeys убирает из keys.json ключи профилей, которые пользователь убрал
// (removed) и которых нет в новых настройках: убранный профиль не оставляет
// секрета на диске. Только по названию в removed: PUT заменяет документ целиком
// и версии не проверяет, поэтому отсутствие профиля в нём может быть и
// устаревшей вкладкой — такой ключ не трогается. Неудача не отменяет
// сохранённых настроек.
func (s *Server) dropKeys(store keys.Store, removed []string, cur config.Config) {
	s.keysMu.Lock()
	defer s.keysMu.Unlock()
	for _, name := range removed {
		if _, kept := cur.Profiles[name]; kept {
			continue
		}
		if err := store.Delete(name); err != nil {
			log.Printf("ключ убранного профиля %s не удалён: %v", name, err)
		}
	}
}

// keyTarget — профиль из пути, только существующий, и хранилище ключей.
func (s *Server) keyTarget(w http.ResponseWriter, r *http.Request) (config.Config, keys.Store, string, bool) {
	name := r.PathValue("profile")
	cfg, store, _ := s.loadConfig() // испорченный конфиг — встроенные профили
	if _, ok := cfg.Profiles[name]; !ok {
		http.Error(w, "нет такого профиля", http.StatusNotFound)
		return cfg, store, "", false
	}
	return cfg, store, name, true
}

// checkKeyText проверяет присланный ключ и срезает пробелы по краям. Ошибка
// ключа не повторяет.
func checkKeyText(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	switch {
	case key == "":
		return "", errors.New("ключ пустой")
	case len(key) > maxKeyBytes:
		return "", fmt.Errorf("ключ длиннее %d байт", maxKeyBytes)
	case hasSpaceOrControl(key):
		return "", errors.New("в ключе пробел, перевод строки или управляющий знак — похоже на неудачную вставку")
	}
	return key, nil
}

// keysUnreadable — отказ записи в испорченный keys.json; файл не тронут.
func keysUnreadable(w http.ResponseWriter, err error) {
	log.Printf("keys.json: %v", err) // в журнале сервера путь есть, в ответе — нет
	http.Error(w, keysUnreadableText+": ключи не изменены. Исправьте файл или удалите его вручную и повторите.",
		http.StatusConflict)
}

func (s *Server) handlePutKey(w http.ResponseWriter, r *http.Request) {
	cfg, store, name, ok := s.keyTarget(w, r)
	if !ok {
		return
	}
	var in struct {
		Key string `json:"key"`
	}
	if err := decodeStrict(r, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	key, err := checkKeyText(in.Key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if p := cfg.Profiles[name]; app.KeySource(p) == "env" {
		http.Error(w, fmt.Sprintf("ключ профиля задан переменной %s и меняется в окружении, а не здесь", p.KeyEnv),
			http.StatusConflict)
		return
	}
	// Чтение и запись keys.json — одна операция: без замка два запроса подряд
	// прочли бы один и тот же файл, и вторая запись стёрла бы первую.
	s.keysMu.Lock()
	defer s.keysMu.Unlock()
	if _, err := store.Get(name); err != nil {
		keysUnreadable(w, err) // Set дошёл бы до того же, но с путём в тексте ошибки
		return
	}
	if err := store.Set(name, key); err != nil {
		log.Printf("ключ профиля %s не записан: %v", name, err)
		http.Error(w, "не удалось записать keys.json", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	_, store, name, ok := s.keyTarget(w, r)
	if !ok {
		return
	}
	s.keysMu.Lock()
	defer s.keysMu.Unlock()
	if _, err := store.Get(name); err != nil {
		keysUnreadable(w, err)
		return
	}
	if err := store.Delete(name); err != nil {
		log.Printf("ключ профиля %s не удалён: %v", name, err)
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

func (s *Server) handleCheckKey(w http.ResponseWriter, r *http.Request) {
	cfg, store, name, ok := s.keyTarget(w, r)
	if !ok {
		return
	}
	fail := func(status int, msg string) {
		out := map[string]any{"ok": false, "message": msg}
		if status != 0 {
			out["status"] = status
		}
		writeJSON(w, out)
	}
	p, err := s.resolve(cfg, store, name)
	if err != nil {
		if ke, ok := isKeysUnreadable(err); ok {
			log.Printf("keys.json: %v", ke.cause)
		}
		fail(0, err.Error()) // у ошибки про keys.json текст без пути
		return
	}
	if app.KeySource(p) == "none" {
		fail(0, fmt.Sprintf("профиль %s: ключа нет — сначала сохраните его", name))
		return
	}
	timeout := s.keyCheckTimeout
	if timeout == 0 {
		timeout = defaultKeyCheckTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	code, err := app.CheckKey(ctx, p, s.httpClient())
	switch {
	case err != nil:
		log.Printf("проверка ключа профиля %s: %v", name, err)
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
