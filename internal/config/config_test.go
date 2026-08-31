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
	if c.Default != "deepseek" {
		t.Fatalf("Default = %q, ожидался deepseek", c.Default)
	}
}

func TestClaudeProfileDefaults(t *testing.T) {
	p := Builtin().Profiles["claude"]
	if p.Kind != KindAnthropic {
		t.Fatalf("Kind = %q, ожидался %q", p.Kind, KindAnthropic)
	}
	if p.Model != "claude-opus-5" {
		t.Fatalf("Model = %q, ожидался claude-opus-5", p.Model)
	}
}

func TestOpenAIProfileHasReasoning(t *testing.T) {
	p := Builtin().Profiles["openai"]
	if p.Kind != KindOpenAI {
		t.Fatalf("Kind = %q, ожидался %q", p.Kind, KindOpenAI)
	}
	if !p.Reasoning {
		t.Fatalf("Reasoning = %v, ожидалось true (gpt-5.6 — рассуждающая модель)", p.Reasoning)
	}
}

func TestResolveEmptyGivesDefault(t *testing.T) {
	p, err := Builtin().Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "deepseek" {
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
	if got := c.Profiles["claude"].Kind; got != KindAnthropic {
		t.Fatalf("Kind затёрт: %q", got)
	}
}

func TestLoadOverridesModelKeepsBaseURLAndKeyEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"profiles":{"openai":{"model":"gpt-5.5"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	openai := c.Profiles["openai"]
	if got := openai.Model; got != "gpt-5.5" {
		t.Fatalf("Model = %q, ожидалось gpt-5.5", got)
	}
	if got := openai.BaseURL; got != "https://api.openai.com/v1" {
		t.Fatalf("BaseURL затёрт: %q", got)
	}
	if got := openai.KeyEnv; got != "OPENAI_API_KEY" {
		t.Fatalf("KeyEnv затёрт: %q", got)
	}
}

func TestLoadNewProfileFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"profiles":{"custom":{"kind":"openai","base_url":"https://custom.api.com","model":"custom-model","key_env":"CUSTOM_KEY"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	custom, ok := c.Profiles["custom"]
	if !ok {
		t.Fatal("профиль custom не создан из файла")
	}
	if got := custom.Kind; got != KindOpenAI {
		t.Fatalf("Kind = %q, ожидался %q", got, KindOpenAI)
	}
	if got := custom.Model; got != "custom-model" {
		t.Fatalf("Model = %q", got)
	}
	// Проверяем, что профиль разрешается
	if _, err := c.Resolve("custom"); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
}

func TestLoadNewProfileMissingKindFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"profiles":{"bad":{"model":"some-model","key_env":"SOME_KEY"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Загрузка должна пройти, но Resolve должна дать ошибку
	if _, err := c.Resolve("bad"); err == nil {
		t.Fatal("ожидалась ошибка при отсутствии kind")
	}
}

func TestLoadReasoningFalseOverridesTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"profiles":{"openai":{"reasoning":false}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	openai := c.Profiles["openai"]
	if openai.Reasoning {
		t.Fatalf("Reasoning = %v, ожидалось false (явный false должен выключить встроенный true)", openai.Reasoning)
	}
}

func TestLoadReasoningAbsentDoesNotChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"profiles":{"openai":{"model":"gpt-5.5"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	openai := c.Profiles["openai"]
	if !openai.Reasoning {
		t.Fatalf("Reasoning = %v, ожидалось true (отсутствие поля должно сохранить встроенное значение)", openai.Reasoning)
	}
}

func TestLoadOverridesMaxOutputTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"profiles":{"openrouter":{"max_output_tokens":4096}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Profiles["openrouter"].MaxOutputTokens; got != 4096 {
		t.Fatalf("MaxOutputTokens = %d, ожидалось 4096", got)
	}
}

func TestLoadMaxOutputTokensAbsentDoesNotChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"profiles":{"openrouter":{"model":"x"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Profiles["openrouter"].MaxOutputTokens; got != 0 {
		t.Fatalf("MaxOutputTokens = %d, ожидалось 0 (умолчание провайдера)", got)
	}
}

func TestBuiltinProfilesHaveNoMaxOutputTokensOverride(t *testing.T) {
	c := Builtin()
	for name, p := range c.Profiles {
		if p.MaxOutputTokens != 0 {
			t.Errorf("встроенный профиль %q имеет MaxOutputTokens=%d, ожидалось 0 (умолчание)",
				name, p.MaxOutputTokens)
		}
	}
}

func TestAPIKeyMissing(t *testing.T) {
	p := Profile{Name: "x", KeyEnv: "ARISTOTLE_TEST_MISSING_KEY"}
	os.Unsetenv(p.KeyEnv)
	if _, err := p.APIKey(); err == nil {
		t.Fatal("ожидалась ошибка при отсутствии переменной")
	}
}
