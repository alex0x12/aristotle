package app

import (
	"strconv"
	"strings"

	"github.com/alex0x12/aristotle/internal/provider"
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
