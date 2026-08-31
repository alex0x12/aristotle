package provider

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSearchToolSchemaIsStable(t *testing.T) {
	// Определение инструмента входит в кэшируемый префикс (tools -> system ->
	// messages), поэтому оно обязано быть неизменным от запроса к запросу.
	a := SearchCorpusTool()
	b := SearchCorpusTool()
	if a.Name != b.Name || a.Description != b.Description {
		t.Fatal("определение инструмента должно быть детерминированным")
	}
	props, ok := a.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("нет properties: %+v", a.Schema)
	}
	for _, key := range []string{"query", "group", "k"} {
		if _, ok := props[key]; !ok {
			t.Errorf("в схеме нет параметра %q", key)
		}
	}
	req, ok := a.Schema["required"].([]string)
	if !ok || len(req) != 1 || req[0] != "query" {
		t.Errorf("required = %+v, ожидался [query]", a.Schema["required"])
	}

	// Главное утверждение: проверки по паре полей выше пропустят расхождение
	// внутри схемы (скажем, в перечне enum у group или в описании k), а
	// именно такое расхождение и разрушит кэш промпта. Поэтому сравниваем
	// определения целиком, побайтово, через сериализацию в JSON.
	aJSON, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("сериализация a: %v", err)
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("сериализация b: %v", err)
	}
	if !bytes.Equal(aJSON, bJSON) {
		t.Fatalf("определения инструмента различаются побайтово:\na = %s\nb = %s", aJSON, bJSON)
	}
}

func TestRequestDefaultsMaxTokens(t *testing.T) {
	r := Request{}
	if got := r.EffectiveMaxTokens(); got != DefaultMaxTokens {
		t.Fatalf("EffectiveMaxTokens = %d, ожидалось %d", got, DefaultMaxTokens)
	}
	r.MaxTokens = 1000
	if got := r.EffectiveMaxTokens(); got != 1000 {
		t.Fatalf("EffectiveMaxTokens = %d", got)
	}
}

func TestEffectiveMaxTokensForUsesProfileOverride(t *testing.T) {
	// Профиль без переопределения — умолчание провайдера.
	if got := (Request{}).EffectiveMaxTokensFor(0); got != DefaultMaxTokens {
		t.Fatalf("EffectiveMaxTokensFor(0) = %d, ожидалось %d", got, DefaultMaxTokens)
	}
	// Профиль с переопределением — используется потолок профиля.
	if got := (Request{}).EffectiveMaxTokensFor(4096); got != 4096 {
		t.Fatalf("EffectiveMaxTokensFor(4096) = %d, ожидалось 4096", got)
	}
	// Явный per-запросный MaxTokens побеждает потолок профиля.
	r := Request{MaxTokens: 500}
	if got := r.EffectiveMaxTokensFor(4096); got != 500 {
		t.Fatalf("EffectiveMaxTokensFor с MaxTokens=500 = %d, ожидалось 500", got)
	}
}
