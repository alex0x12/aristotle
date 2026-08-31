package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinHasFourProfiles(t *testing.T) {
	c := Builtin()
	for _, name := range []string{"claude", "deepseek", "openai", "openrouter"} {
		if _, ok := c.Profiles[name]; !ok {
			t.Fatalf("нет встроенного профиля %q", name)
		}
	}
	if c.Default != "claude" {
		t.Fatalf("Default = %q, ожидался claude", c.Default)
	}
}

func TestClaudeProfileDefaults(t *testing.T) {
	p := Builtin().Profiles["claude"]
	if p.Kind != "anthropic" {
		t.Fatalf("Kind = %q", p.Kind)
	}
	if p.Model != "claude-opus-5" {
		t.Fatalf("Model = %q, ожидался claude-opus-5", p.Model)
	}
}

func TestResolveEmptyGivesDefault(t *testing.T) {
	p, err := Builtin().Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "claude" {
		t.Fatalf("Name = %q", p.Name)
	}
}

func TestResolveUnknownFails(t *testing.T) {
	if _, err := Builtin().Resolve("gpt-9"); err == nil {
		t.Fatal("ожидалась ошибка для неизвестного профиля")
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "нет-такого.json"))
	if err != nil {
		t.Fatalf("отсутствие файла не должно быть ошибкой: %v", err)
	}
	if len(c.Profiles) == 0 {
		t.Fatal("встроенные профили должны остаться")
	}
}

func TestLoadOverridesModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"default":"deepseek","profiles":{"claude":{"model":"claude-sonnet-5"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Default != "deepseek" {
		t.Fatalf("Default = %q", c.Default)
	}
	if got := c.Profiles["claude"].Model; got != "claude-sonnet-5" {
		t.Fatalf("Model = %q", got)
	}
	if got := c.Profiles["claude"].Kind; got != "anthropic" {
		t.Fatalf("Kind затёрт: %q", got)
	}
}

func TestAPIKeyMissing(t *testing.T) {
	p := Profile{Name: "x", KeyEnv: "ARISTOTLE_TEST_MISSING_KEY"}
	os.Unsetenv(p.KeyEnv)
	if _, err := p.APIKey(); err == nil {
		t.Fatal("ожидалась ошибка при отсутствии переменной")
	}
}
