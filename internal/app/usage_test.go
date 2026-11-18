package app

import (
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/provider"
)

func TestFormatTokensGroupsThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1 000", 12400: "12 400", 1234567: "1 234 567"} {
		if got := FormatTokens(n); got != want {
			t.Errorf("FormatTokens(%d) = %q, ожидалось %q", n, got, want)
		}
	}
}

func TestUsageLine(t *testing.T) {
	u := provider.Usage{Input: 2000, Output: 350, CacheRead: 18000, Known: true}
	if got := UsageLine(u); got != "[токены: вход 2 000 · кэш 18 000 · вывод 350]" {
		t.Fatalf("UsageLine = %q", got)
	}
	// Запись в кэш — след старых журналов: колонки «в кэш» больше нет, цены тоже.
	u.CacheWrite = 300
	if got := UsageLine(u); got != "[токены: вход 2 000 · кэш 18 000 · вывод 350]" || strings.Contains(got, "$") {
		t.Fatalf("UsageLine = %q", got)
	}
	if got := UsageLine(provider.Usage{}); got != "[токены: провайдер не сообщил]" {
		t.Fatalf("UsageLine = %q", got)
	}
}
