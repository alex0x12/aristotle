package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// openCodeBaseURL — OpenAI-совместимый адрес бесплатных моделей OpenCode.
// Ключ для них — "public"; приложение шлёт его как обычный Bearer.
const openCodeBaseURL = "https://opencode.ai/inference/openai/v1"

type freeModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type freeModelsView struct {
	BaseURL string      `json:"base_url"`
	Models  []freeModel `json:"models"`
	Source  string      `json:"source"` // cli, cache, fallback
}

// fallbackFree — список на случай, если ни opencode, ни его кеш недоступны.
// Он не истина в последней инстанции: CLI и кеш важнее.
var fallbackFree = []freeModel{
	{"beam-free", "Beam Free"},
	{"deepseek-v4.1-flash-free", "DeepSeek V4.1 Flash Free"},
	{"flint-40b-free", "Flint 40B Free"},
	{"hy4-flash-free", "HY4 Flash Free"},
	{"iceman-free", "Iceman Free"},
	{"longcat-2.5-free", "LongCat 2.5 Free"},
	{"mimo-v2.6-flash-free", "MiMo v2.6 Flash Free"},
	{"nemotron-3.5-lightning-free", "Nemotron 3.5 Lightning Free"},
	{"nemotron-3.5-super-vl-free", "Nemotron 3.5 Super VL Free"},
	{"tidepool-alpha-free", "Tidepool Alpha Free"},
}

// parseOpenCodeModels берёт строки `opencode/<id>-free` из `opencode models`.
// Провайдер `opencode-go` не берём: это другой шлюз.
func isFreeModel(id string) bool {
	return strings.HasSuffix(id, "-free") || strings.HasSuffix(id, ":free")
}

func parseOpenCodeModels(out string) []freeModel {
	var models []freeModel
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		id, ok := strings.CutPrefix(strings.TrimSpace(line), "opencode/")
		if !ok || !isFreeModel(id) || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, freeModel{ID: id, Name: id})
	}
	return models
}

// freeModelsFromCache читает models.json, который OpenCode держит в кеше.
// Формат чужой, поэтому любая неожиданность — просто «нет данных».
func freeModelsFromCache(path string) []freeModel {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var all map[string]struct {
		Models map[string]struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Cost struct {
				Input float64 `json:"input"`
			} `json:"cost"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil
	}
	var models []freeModel
	for id, m := range all["opencode"].Models {
		if !isFreeModel(id) || m.Cost.Input != 0 {
			continue
		}
		name := m.Name
		if name == "" {
			name = id
		}
		models = append(models, freeModel{ID: id, Name: name})
	}
	return models
}

func defaultOpenCodeModels() ([]freeModel, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "opencode", "models")
	cmd.WaitDelay = 2 * time.Second
	if out, err := cmd.Output(); err == nil {
		if models := parseOpenCodeModels(string(out)); len(models) > 0 {
			return models, "cli"
		}
	}
	cache, err := os.UserCacheDir()
	if err == nil {
		if models := freeModelsFromCache(filepath.Join(cache, "opencode", "models.json")); len(models) > 0 {
			return models, "cache"
		}
	}
	return fallbackFree, "fallback"
}

// freeModels — список для экрана настроек. Тест подменяет поле Server.
func (s *Server) freeModels() ([]freeModel, string) {
	if s.OpenCodeModels != nil {
		return s.OpenCodeModels()
	}
	return defaultOpenCodeModels()
}

func (s *Server) handleOpenCodeModels(w http.ResponseWriter, r *http.Request) {
	models, source := s.freeModels()
	writeJSON(w, freeModelsView{BaseURL: openCodeBaseURL, Models: models, Source: source})
}
