package app

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/alex0x12/aristotle/internal/config"
)

// anthropicOrigin — адрес API Anthropic, на который ходит провайдер (умолчание
// SDK). Окружение ANTHROPIC_BASE_URL, которое SDK тоже читает, проверка не
// учитывает намеренно: ключ уходит только на этот адрес.
const anthropicOrigin = "https://api.anthropic.com"

// CheckKey проверяет ключ профиля бесплатным запросом — списком моделей.
// Возвращает код ответа провайдера: 200 — ключ принят.
//
// Перенаправления не выполняются: Go срезает при переходе на другой адрес
// только Authorization, а x-api-key (Anthropic) ушёл бы туда целиком. Ответ 3xx
// возвращается как есть и читается как отказ.
func CheckKey(ctx context.Context, p config.Profile, client *http.Client) (int, error) {
	key, err := p.APIKey()
	if err != nil {
		return 0, err
	}
	var url string
	header := http.Header{}
	switch p.Kind {
	case config.KindAnthropic:
		// Адрес профиля не читается: Anthropic-провайдер его не использует,
		// и проверка ключа не должна отправить его туда, куда не ходят запросы.
		url = anthropicOrigin + "/v1/models"
		header.Set("x-api-key", key)
		header.Set("anthropic-version", "2023-06-01")
	default:
		url = strings.TrimSuffix(p.BaseURL, "/") + "/models"
		header.Set("Authorization", "Bearer "+key)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header = header
	c := *client // копия: чужой клиент (общий) не меняем
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, nil
}
