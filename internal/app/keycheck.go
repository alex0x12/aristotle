package app

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/alex0x12/aristotle/internal/config"
)

// maxCheckBody — сколько тела ответа проверки читается и выбрасывается: чтобы
// соединение можно было переиспользовать, но бесконечный ответ не держал память.
const maxCheckBody = 1 << 20

// CheckKey проверяет ключ бесплатным запросом — списком моделей по
// настроенному адресу. Возвращает код ответа провайдера: 200 — ключ принят.
// Тело ответа не возвращается вовсе: оно может повторять заголовки и ключ.
//
// Перенаправления не выполняются: ответ 3xx возвращается как есть и читается
// как отказ — ключ не должен уйти туда, куда его не посылали. Уходит только
// заголовок Authorization: Bearer. Адрес проверяется так же, как при
// сохранении настроек.
func CheckKey(ctx context.Context, baseURL, key string, client *http.Client) (int, error) {
	if key == "" {
		return 0, ErrNoKey
	}
	if err := config.ValidateBaseURL(baseURL); err != nil {
		return 0, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/models", nil)
	if err != nil {
		return 0, err
	}
	req.Header = http.Header{"Authorization": {"Bearer " + key}}
	c := *client // копия: чужой клиент (общий) не меняем
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxCheckBody))
	return resp.StatusCode, nil
}
