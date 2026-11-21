package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOpenCodeModelsKeepsOnlyFreeOpenCode(t *testing.T) {
	out := strings.Join([]string{
		"opencode/beam-free",
		"opencode/deepseek-v4.1-flash-free",
		"opencode-go/beam-free", // другой шлюз
		"other/beam-free",
		"opencode/gpt-5.6-luna", // платная
		"  opencode/hy4-flash-free  ",
		"opencode/beam-free", // дубль
	}, "\n")
	got := parseOpenCodeModels(out)
	if len(got) != 3 {
		t.Fatalf("модели: %+v", got)
	}
	for _, m := range got {
		if !strings.HasSuffix(m.ID, "-free") || strings.Contains(m.ID, "opencode-go") {
			t.Errorf("лишняя модель %q", m.ID)
		}
	}
}

func TestFreeModelsFromCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	data := `{
	  "opencode": {"models": {
	    "beam-free": {"id":"beam-free","name":"Beam Free","cost":{"input":0}},
	    "paid": {"id":"paid","name":"Paid","cost":{"input":1}},
	    "not-free-name": {"id":"not-free-name","name":"X","cost":{"input":0}}
	  }},
	  "opencode-go": {"models": {"other-free": {"id":"other-free","cost":{"input":0}}}}
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	got := freeModelsFromCache(path)
	if len(got) != 1 || got[0].ID != "beam-free" || got[0].Name != "Beam Free" {
		t.Fatalf("из кеша: %+v", got)
	}
}

func TestOpenCodeModelsEndpoint(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.OpenCodeModels = func() ([]freeModel, string) {
		return []freeModel{{ID: "beam-free", Name: "Beam Free"}}, "test"
	}
	rec := do(t, s.Handler(), http.MethodGet, "/api/opencode/models", "")
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var got freeModelsView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != openCodeBaseURL || got.Source != "test" ||
		len(got.Models) != 1 || got.Models[0].ID != "beam-free" {
		t.Fatalf("ответ: %+v", got)
	}
}
