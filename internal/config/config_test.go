package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaults(t *testing.T) {
	d := Default()
	want := Config{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", ReasoningEffort: "high", ContextBudget: 128000}
	if d != want {
		t.Fatalf("Default() = %+v, ожидалось %+v", d, want)
	}
	if DefaultBaseURL != want.BaseURL || DefaultModel != want.Model || DefaultContextBudget != 128000 || DefaultCorpusK != 6 ||
		DefaultReasoningEffort != want.ReasoningEffort {
		t.Fatal("константы умолчаний разошлись с Default()")
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("умолчания не проходят проверку: %v", err)
	}
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || c != Default() {
		t.Fatalf("Load(нет файла) = %+v, %v", c, err)
	}
}

// Файл накладывается ПОВЕРХ умолчаний: опущенное поле остаётся умолчанием, а
// явный ноль бюджета («без урезания») — нулём. Файлы, записанные до появления
// усилия, получают умолчание high.
func TestLoadOverlaysDefaults(t *testing.T) {
	cases := []struct {
		name, body string
		want       Config
	}{
		{"пустой объект", `{}`, Default()},
		{"только модель", `{"model":"deepseek-v4-pro"}`,
			Config{BaseURL: DefaultBaseURL, Model: "deepseek-v4-pro", ReasoningEffort: DefaultReasoningEffort, ContextBudget: DefaultContextBudget}},
		{"только адрес", `{"base_url":"https://openrouter.ai/api/v1"}`,
			Config{BaseURL: "https://openrouter.ai/api/v1", Model: DefaultModel, ReasoningEffort: DefaultReasoningEffort, ContextBudget: DefaultContextBudget}},
		{"явный ноль бюджета", `{"context_budget":0}`,
			Config{BaseURL: DefaultBaseURL, Model: DefaultModel, ReasoningEffort: DefaultReasoningEffort, ContextBudget: 0}},
		{"только усилие", `{"reasoning_effort":"max"}`,
			Config{BaseURL: DefaultBaseURL, Model: DefaultModel, ReasoningEffort: "max", ContextBudget: DefaultContextBudget}},
		{"всё", `{"base_url":"http://localhost:11434/v1","model":"qwen3.5:4b","reasoning_effort":"low","context_budget":32000}`,
			Config{BaseURL: "http://localhost:11434/v1", Model: "qwen3.5:4b", ReasoningEffort: "low", ContextBudget: 32000}},
		{"пробелы вокруг модели срезаются", `{"model":"  m1  "}`,
			Config{BaseURL: DefaultBaseURL, Model: "m1", ReasoningEffort: DefaultReasoningEffort, ContextBudget: DefaultContextBudget}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Load(writeCfg(t, tc.body))
			if err != nil || got != tc.want {
				t.Fatalf("Load = %+v, %v; ожидалось %+v", got, err, tc.want)
			}
		})
	}
}

// Старый формат (профили) и поле из будущей версии — ошибка с названием поля:
// именно она отправляет веб-интерфейс в запасной режим с предупреждением, а
// командную строку — на умолчания с одной строкой на stderr.
func TestLoadRejectsOldFormatAndUnknownFields(t *testing.T) {
	cases := []struct{ name, body, field string }{
		{"profiles", `{"default":"deepseek","profiles":{"deepseek":{"model":"x"}}}`, "default"},
		{"только profiles", `{"profiles":{}}`, "profiles"},
		{"corpus_k", `{"corpus_k":6}`, "corpus_k"},
		{"будущее поле", `{"model":"x","thinking":"max"}`, "thinking"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeCfg(t, tc.body))
			if err == nil {
				t.Fatal("ожидалась ошибка")
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("ошибка не называет поле %q: %v", tc.field, err)
			}
		})
	}
}

func TestLoadRejectsGarbage(t *testing.T) {
	for name, body := range map[string]string{
		"пустой файл":    ``,
		"пробелы":        "  \n",
		"не JSON":        `not json`,
		"массив":         `[]`,
		"строка":         `"x"`,
		"лишнее после":   `{"model":"x"} {"model":"y"}`,
		"неверный тип":   `{"context_budget":"много"}`,
		"оборванный":     `{"model":`,
		"бюджет дробный": `{"context_budget":1.5}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeCfg(t, body)); err == nil {
				t.Fatalf("%q должен читаться с ошибкой", body)
			}
		})
	}
}

// Файл, прочитанный без ошибки разбора, но с недопустимыми значениями, тоже
// ошибка: Load проверяет то же, что веб при сохранении.
func TestLoadValidatesValues(t *testing.T) {
	for name, body := range map[string]string{
		"пустая модель":  `{"model":""}`,
		"отрицательный":  `{"context_budget":-1}`,
		"ftp":            `{"base_url":"ftp://x/v1"}`,
		"пустой адрес":   `{"base_url":""}`,
		"без хоста":      `{"base_url":"https:///v1"}`,
		"логин в адресе": `{"base_url":"https://user:pw@api.example.com/v1"}`,
		"усилие medium":  `{"reasoning_effort":"medium"}`,
		"усилие пустое":  `{"reasoning_effort":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeCfg(t, body)); err == nil {
				t.Fatalf("%q должен отвергаться", body)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	ok := Default()
	long := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		name  string
		mod   func(*Config)
		field string // пусто — конфиг допустим
	}{
		{"умолчания", func(*Config) {}, ""},
		{"бюджет 0", func(c *Config) { c.ContextBudget = 0 }, ""},
		{"бюджет верхний предел", func(c *Config) { c.ContextBudget = 10_000_000 }, ""},
		{"бюджет выше предела", func(c *Config) { c.ContextBudget = 10_000_001 }, "context_budget"},
		{"бюджет отрицательный", func(c *Config) { c.ContextBudget = -1 }, "context_budget"},
		{"усилие low", func(c *Config) { c.ReasoningEffort = "low" }, ""},
		{"усилие max", func(c *Config) { c.ReasoningEffort = "max" }, ""},
		{"усилие пустое", func(c *Config) { c.ReasoningEffort = "" }, "reasoning_effort"},
		{"усилие medium", func(c *Config) { c.ReasoningEffort = "medium" }, "reasoning_effort"},
		{"усилие заглавными", func(c *Config) { c.ReasoningEffort = "MAX" }, "reasoning_effort"},
		{"модель пустая", func(c *Config) { c.Model = "" }, "model"},
		{"модель из пробелов", func(c *Config) { c.Model = "   " }, "model"},
		{"модель 200 знаков", func(c *Config) { c.Model = long(200) }, ""},
		{"модель 201 знак", func(c *Config) { c.Model = long(201) }, "model"},
		{"модель 200 рун по-русски", func(c *Config) { c.Model = strings.Repeat("я", 200) }, ""},
		{"модель с переводом строки", func(c *Config) { c.Model = "a\nb" }, "model"},
		{"модель с NUL", func(c *Config) { c.Model = "a\x00b" }, "model"},
		{"модель с табуляцией", func(c *Config) { c.Model = "a\tb" }, "model"},
		{"модель с пробелом внутри", func(c *Config) { c.Model = "gpt 5" }, ""},
		{"адрес пустой", func(c *Config) { c.BaseURL = "" }, "base_url"},
		{"адрес без схемы", func(c *Config) { c.BaseURL = "api.deepseek.com/v1" }, "base_url"},
		{"адрес ftp", func(c *Config) { c.BaseURL = "ftp://api.example.com" }, "base_url"},
		{"адрес javascript", func(c *Config) { c.BaseURL = "javascript:alert(1)" }, "base_url"},
		{"адрес file", func(c *Config) { c.BaseURL = "file:///etc/passwd" }, "base_url"},
		{"адрес без хоста", func(c *Config) { c.BaseURL = "https://" }, "base_url"},
		{"адрес только порт", func(c *Config) { c.BaseURL = "http://:8080/v1" }, "base_url"},
		{"адрес с логином", func(c *Config) { c.BaseURL = "https://u@api.example.com/v1" }, "base_url"},
		{"адрес с логином и паролем", func(c *Config) { c.BaseURL = "https://u:p@api.example.com/v1" }, "base_url"},
		{"адрес с пробелом", func(c *Config) { c.BaseURL = "https://api.example.com/v 1" }, "base_url"},
		{"адрес с переводом строки", func(c *Config) { c.BaseURL = "https://api.example.com/v1\n" }, "base_url"},
		{"адрес с NUL", func(c *Config) { c.BaseURL = "https://api.example.com/\x00" }, "base_url"},
		{"адрес 2048 знаков", func(c *Config) { c.BaseURL = "https://a.example/" + long(2048-len("https://a.example/")) }, ""},
		{"адрес 2049 знаков", func(c *Config) { c.BaseURL = "https://a.example/" + long(2049-len("https://a.example/")) }, "base_url"},
		{"адрес http локальный", func(c *Config) { c.BaseURL = "http://127.0.0.1:11434/v1" }, ""},
		{"адрес https openrouter", func(c *Config) { c.BaseURL = "https://openrouter.ai/api/v1" }, ""},
		{"адрес с хвостовым слэшем", func(c *Config) { c.BaseURL = "https://api.example.com/v1/" }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ok
			tc.mod(&c)
			err := c.Validate()
			switch {
			case tc.field == "" && err != nil:
				t.Fatalf("допустимый конфиг отвергнут: %v", err)
			case tc.field != "" && err == nil:
				t.Fatal("недопустимый конфиг принят")
			case tc.field != "" && !strings.HasPrefix(err.Error(), tc.field+":"):
				t.Fatalf("ошибка должна начинаться с названия поля %q: %v", tc.field, err)
			}
		})
	}
}

func TestValidateAcceptsOpenCodeCLI(t *testing.T) {
	c := Default()
	c.BaseURL = OpenCodeCLI
	c.Model = "beam-free"
	if err := c.Validate(); err != nil {
		t.Fatalf("opencode-cli отвергнут: %v", err)
	}
}

// Ошибка проверки не повторяет присланное значение: в адресе мог оказаться
// пароль, а в модели — вставленный по ошибке ключ.
func TestValidateErrorDoesNotEchoValue(t *testing.T) {
	c := Default()
	c.BaseURL = "https://user:SECRET-PASSWORD-777@api.example.com/v1"
	if err := c.Validate(); err == nil || strings.Contains(err.Error(), "SECRET-PASSWORD-777") {
		t.Fatalf("ошибка повторяет значение: %v", err)
	}
	c = Default()
	c.Model = "sk-FAKE-KEY-123\nx"
	if err := c.Validate(); err == nil || strings.Contains(err.Error(), "sk-FAKE-KEY-123") {
		t.Fatalf("ошибка повторяет значение: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	for _, c := range []Config{
		Default(),
		{BaseURL: "https://openrouter.ai/api/v1", Model: "anthropic/claude-opus-5", ReasoningEffort: "max", ContextBudget: 0},
		{BaseURL: "http://localhost:11434/v1", Model: "qwen3.5:4b", ReasoningEffort: "low", ContextBudget: 10_000_000},
	} {
		path := filepath.Join(t.TempDir(), "sub", "config.json")
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil || got != c {
			t.Fatalf("Load(Save(%+v)) = %+v, %v", c, got, err)
		}
	}
}

// Save пишет документ целиком — все три поля, в том числе равные умолчаниям, —
// чтобы файл читался сам по себе и не менялся с умолчаниями программы.
func TestSaveWritesFullDocumentWithMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, field := range []string{`"base_url"`, `"model"`, `"context_budget"`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("в файле нет %s:\n%s", field, data)
		}
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("права %v, %v", st.Mode().Perm(), err)
	}
	// Бюджет 0 («без урезания») тоже пишется явно, а не пропадает как пустой.
	c := Default()
	c.ContextBudget = 0
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), `"context_budget": 0`) {
		t.Fatalf("нулевой бюджет не записан:\n%s", data)
	}
}

// Недопустимый конфиг не записывается и существующий файл не трогает.
func TestSaveRefusesInvalidAndKeepsFile(t *testing.T) {
	path := writeCfg(t, `{"model":"keep-me"}`)
	bad := Default()
	bad.BaseURL = "ftp://x"
	if err := Save(path, bad); err == nil {
		t.Fatal("недопустимый конфиг записан")
	}
	if data, _ := os.ReadFile(path); string(data) != `{"model":"keep-me"}` {
		t.Fatalf("файл изменился: %s", data)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("в каталоге лишние файлы: %v", entries)
	}
}

// Окружение на конфиг не влияет: ни ключей, ни адресов оттуда не берётся.
func TestLoadIgnoresEnvironment(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "FAKE-ENV-KEY")
	t.Setenv("OPENAI_BASE_URL", "https://evil.example/v1")
	t.Setenv("ANTHROPIC_API_KEY", "FAKE-ENV-KEY")
	c, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || c != Default() {
		t.Fatalf("Load = %+v, %v", c, err)
	}
}
