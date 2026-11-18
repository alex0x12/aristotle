package web

import (
	"net/http"
	"os"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
)

// keyView — всё, что интерфейс знает о ключе. Самого ключа здесь нет.
type keyView struct {
	Source string `json:"source"` // env, file, none
	Hint   string `json:"hint,omitempty"`
}

type profileView struct {
	Name            string         `json:"name"`
	Builtin         bool           `json:"builtin"`
	Kind            string         `json:"kind"`
	BaseURL         string         `json:"base_url"`
	Model           string         `json:"model"`
	KeyEnv          string         `json:"key_env"`
	Reasoning       bool           `json:"reasoning"`
	ReasoningEffort string         `json:"reasoning_effort"`
	Temperature     *float64       `json:"temperature"`
	MaxOutputTokens int            `json:"max_output_tokens"`
	ContextBudget   int            `json:"context_budget"`
	Prices          *config.Prices `json:"prices"`
	Key             keyView        `json:"key"`
}

func viewOf(p config.Profile) profileView {
	_, builtin := config.Builtin().Profiles[p.Name]
	v := profileView{
		Name: p.Name, Builtin: builtin, Kind: p.Kind, BaseURL: p.BaseURL, Model: p.Model,
		KeyEnv: p.KeyEnv, Reasoning: p.Reasoning, ReasoningEffort: p.ReasoningEffort,
		Temperature: p.Temperature, MaxOutputTokens: p.MaxOutputTokens,
		ContextBudget: p.ContextBudget, Prices: p.Prices,
	}
	switch app.KeySource(p) {
	case "env":
		v.Key = keyView{Source: "env", Hint: keys.Hint(os.Getenv(p.KeyEnv))}
	case "file":
		v.Key = keyView{Source: "file", Hint: keys.Hint(p.Key)}
	default:
		v.Key = keyView{Source: "none"}
	}
	return v
}

// profiles — все профили с ключами из keys.json, по имени.
func (s *Server) profiles() (config.Config, []profileView, error) {
	cfg, store, err := s.loadConfig()
	if err != nil {
		return cfg, nil, err
	}
	var views []profileView
	for _, name := range app.ProfileNames(cfg) {
		p, err := app.Resolve(cfg, store, name)
		if err != nil {
			return cfg, nil, err
		}
		views = append(views, viewOf(p))
	}
	return cfg, views, nil
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	cfg, views, err := s.profiles()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"default": cfg.Default, "corpus_k": cfg.CorpusK, "profiles": views,
	})
}
