package app

import (
	"strconv"
	"strings"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/render"
)

// FormatTokens — число с пробелами между тысячами: 12 400.
func FormatTokens(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + FormatTokens(-n)
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// UsageLine — строка расхода для командной строки после ответа.
func UsageLine(u provider.Usage) string {
	if !u.Known {
		return "[токены: провайдер не сообщил]"
	}
	parts := []string{"вход " + FormatTokens(u.Input)}
	if u.CacheRead > 0 {
		parts = append(parts, "кэш "+FormatTokens(u.CacheRead))
	}
	if u.CacheWrite > 0 {
		parts = append(parts, "в кэш "+FormatTokens(u.CacheWrite))
	}
	parts = append(parts, "вывод "+FormatTokens(u.Output))
	return "[токены: " + strings.Join(parts, " · ") + "]"
}

// Cost — стоимость расхода по ценам профиля в долларах; nil — цен нет или
// расход неизвестен.
func Cost(p *config.Prices, u provider.Usage) *float64 {
	if p == nil || !u.Known {
		return nil
	}
	c := (float64(u.Input)*p.Input + float64(u.Output)*p.Output +
		float64(u.CacheRead)*p.CacheRead + float64(u.CacheWrite)*p.CacheWrite) / 1e6
	return &c
}

// QuotesOf — врезки ответа для журнала.
func QuotesOf(blocks []render.Block) []chat.Quote {
	var out []chat.Quote
	for _, b := range blocks {
		if b.Kind != render.BlockQuote {
			continue
		}
		out = append(out, chat.Quote{Cite: b.Cite, En: b.En, Ru: b.Ru, Verified: b.Verified, Chapter: b.Chapter})
	}
	return out
}
