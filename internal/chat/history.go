package chat

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alex0x12/aristotle/internal/provider"
)

// Роли служебных записей журнала. Восстановление беседы их пропускает,
// кроме retract.
const (
	RoleMeta    = "meta"    // заголовок беседы; действует последний
	RoleTrim    = "trim"    // где проведена граница контекста
	RoleRetract = "retract" // «ещё раз»: последний вопрос с ответом отозваны
)

// Quote — врезка ответа с результатом сверки. Сверка зависит от глав,
// пришедших на этом ходу, а они не сохраняются, поэтому результат пишется
// в журнал сразу.
type Quote struct {
	Cite     string `json:"cite"`
	En       string `json:"en"`
	Ru       string `json:"ru"`
	Verified bool   `json:"verified"`
	Chapter  string `json:"chapter,omitempty"`
}

type Record struct {
	Time  time.Time `json:"time"`
	Role  string    `json:"role"`
	Text  string    `json:"text"`
	Title string    `json:"title,omitempty"`
	// Profile и CostUSD — только чтение: так писали журналы до «одной модели без
	// профилей и цен». Append их не пишет никогда, а старые записи читаются как
	// раньше и ни на что не влияют.
	Profile    string          `json:"profile,omitempty"`
	Model      string          `json:"model,omitempty"`
	Usage      *provider.Usage `json:"usage,omitempty"`
	CostUSD    *float64        `json:"cost_usd,omitempty"`
	Quotes     []Quote         `json:"quotes,omitempty"`
	StopReason string          `json:"stop_reason,omitempty"` // end, cancelled, error
	Excerpts   int             `json:"excerpts,omitempty"`    // trim
	Turns      int             `json:"turns,omitempty"`       // trim
}

const titleLen = 60

// CutTitle — строка в одну линию не длиннее 60 символов, с многоточием.
func CutTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= titleLen {
		return s
	}
	return strings.TrimSpace(string(r[:titleLen-1])) + "…"
}

// TitleOf — заголовок беседы: последняя запись meta, иначе первый вопрос,
// иначе пусто.
func TitleOf(records []Record) string {
	title := ""
	for _, r := range records {
		if r.Role == RoleMeta && strings.TrimSpace(r.Title) != "" {
			title = r.Title
		}
	}
	if title != "" {
		return title
	}
	for _, r := range records {
		if r.Role == "user" && strings.TrimSpace(r.Text) != "" {
			return CutTitle(r.Text)
		}
	}
	return ""
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
	// Поля только для чтения старых журналов в новые записи не попадают.
	rec.Profile, rec.CostUSD = "", nil
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
