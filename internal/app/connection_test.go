package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{
		Config:  filepath.Join(dir, "config.json"),
		Keys:    filepath.Join(dir, "keys.json"),
		History: filepath.Join(dir, "history"),
	}
}

func TestErrNoKeyText(t *testing.T) {
	const want = "ключ API не задан: откройте «Настройки» в веб-интерфейсе или выполните `aristotle key`"
	if ErrNoKey.Error() != want {
		t.Fatalf("ErrNoKey = %q", ErrNoKey)
	}
}

func TestLoadConnectionDefaultsWithoutFiles(t *testing.T) {
	conn, cfgErr, err := LoadConnection(testPaths(t))
	if cfgErr != nil || err != nil {
		t.Fatalf("cfgErr=%v err=%v", cfgErr, err)
	}
	want := Connection{BaseURL: config.DefaultBaseURL, Model: config.DefaultModel,
		ReasoningEffort: config.DefaultReasoningEffort, ContextBudget: config.DefaultContextBudget}
	if conn != want {
		t.Fatalf("conn = %+v, ожидалось %+v", conn, want)
	}
}

func TestLoadConnectionReadsConfigAndKey(t *testing.T) {
	paths := testPaths(t)
	if err := config.Save(paths.Config, config.Config{BaseURL: "http://127.0.0.1:1/v1", Model: "m", ReasoningEffort: "max", ContextBudget: 0}); err != nil {
		t.Fatal(err)
	}
	if err := (keys.Store{Path: paths.Keys}).Set("FAKE-KEY-AAAA"); err != nil {
		t.Fatal(err)
	}
	conn, cfgErr, err := LoadConnection(paths)
	if cfgErr != nil || err != nil {
		t.Fatalf("cfgErr=%v err=%v", cfgErr, err)
	}
	want := Connection{BaseURL: "http://127.0.0.1:1/v1", Model: "m", Key: "FAKE-KEY-AAAA", ReasoningEffort: "max", ContextBudget: 0}
	if conn != want {
		t.Fatalf("conn = %+v", conn)
	}
}

// Старый config.json (профили) — не отказ: подключение на умолчаниях, а ошибка
// отдельно, чтобы вызывающий мог предупредить.
func TestLoadConnectionOldConfigFallsBackToDefaults(t *testing.T) {
	paths := testPaths(t)
	os.WriteFile(paths.Config, []byte(`{"default":"deepseek","profiles":{"deepseek":{"model":"x"}}}`), 0o600)
	(keys.Store{Path: paths.Keys}).Set("FAKE-KEY-AAAA")
	conn, cfgErr, err := LoadConnection(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfgErr == nil {
		t.Fatal("ошибка конфигурации потеряна")
	}
	if conn.BaseURL != config.DefaultBaseURL || conn.Model != config.DefaultModel || conn.Key != "FAKE-KEY-AAAA" {
		t.Fatalf("conn = %+v", conn)
	}
}

func TestLoadConnectionCorruptKeysFile(t *testing.T) {
	paths := testPaths(t)
	os.WriteFile(paths.Keys, []byte(`{"key": "CORRUPT-FAKE" oops`), 0o600)
	conn, _, err := LoadConnection(paths)
	var ue *keys.UnreadableError
	if !errors.As(err, &ue) {
		t.Fatalf("ожидалась *keys.UnreadableError, получено %v", err)
	}
	if conn.Key != "" || strings.Contains(err.Error(), "CORRUPT") {
		t.Fatalf("conn = %+v, err = %v", conn, err)
	}
}

// Окружение ключей не даёт: ни DEEPSEEK_API_KEY, ни OPENAI_API_KEY, ни прочих.
func TestEnvironmentNeverSuppliesKey(t *testing.T) {
	for _, name := range []string{"DEEPSEEK_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY", "API_KEY", "ARISTOTLE_API_KEY"} {
		t.Setenv(name, "FAKE-ENV-KEY-1234")
	}
	conn, _, err := LoadConnection(testPaths(t))
	if err != nil || conn.Key != "" {
		t.Fatalf("ключ из окружения: %+v, %v", conn, err)
	}
	if _, err := NewProvider(conn); !errors.Is(err, ErrNoKey) {
		t.Fatalf("NewProvider без keys.json = %v, ожидалась ErrNoKey", err)
	}
}

func TestNewProvider(t *testing.T) {
	ok := Connection{BaseURL: "https://api.example.com/v1", Model: "m", Key: "FAKE-KEY-1234", ReasoningEffort: "high", ContextBudget: 1000}
	if p, err := NewProvider(ok); err != nil || p == nil {
		t.Fatalf("NewProvider: %v, %v", p, err)
	}
	cases := map[string]func(*Connection){
		"нет ключа":          func(c *Connection) { c.Key = "" },
		"ключ с пробелом":    func(c *Connection) { c.Key = "FAKE KEY" },
		"адрес пустой":       func(c *Connection) { c.BaseURL = "" },
		"адрес ftp":          func(c *Connection) { c.BaseURL = "ftp://x/v1" },
		"адрес с паролем":    func(c *Connection) { c.BaseURL = "https://u:p@x/v1" },
		"модель пустая":      func(c *Connection) { c.Model = "" },
		"бюджет отрицателен": func(c *Connection) { c.ContextBudget = -5 },
		"усилие неизвестное": func(c *Connection) { c.ReasoningEffort = "medium" },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			c := ok
			mod(&c)
			p, err := NewProvider(c)
			if err == nil || p != nil {
				t.Fatalf("ожидалась ошибка: %v, %v", p, err)
			}
			if strings.Contains(err.Error(), "FAKE") {
				t.Fatalf("ошибка повторяет ключ: %v", err)
			}
		})
	}
	if _, err := NewProvider(Connection{BaseURL: ok.BaseURL, Model: "m"}); !errors.Is(err, ErrNoKey) {
		t.Fatalf("без ключа ожидалась ErrNoKey, получено %v", err)
	}
}

func TestNewProviderOpenCodeCLINeedsNoKey(t *testing.T) {
	p, err := NewProvider(Connection{BaseURL: config.OpenCodeCLI, Model: "beam-free",
		ReasoningEffort: "high", ContextBudget: 1000})
	if err != nil || p == nil {
		t.Fatalf("opencode-cli без ключа: %v, %v", p, err)
	}
}
