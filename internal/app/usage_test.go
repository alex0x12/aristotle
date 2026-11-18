package app

import (
	"testing"

	"github.com/alex0x12/aristotle/internal/config"
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
	u.CacheWrite = 300
	if got := UsageLine(u); got != "[токены: вход 2 000 · кэш 18 000 · в кэш 300 · вывод 350]" {
		t.Fatalf("UsageLine = %q", got)
	}
	if got := UsageLine(provider.Usage{}); got != "[токены: провайдер не сообщил]" {
		t.Fatalf("UsageLine = %q", got)
	}
}

func TestCostPerMillion(t *testing.T) {
	prices := &config.Prices{Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 1.25}
	u := provider.Usage{Input: 1_000_000, Output: 500_000, CacheRead: 2_000_000, CacheWrite: 400_000, Known: true}
	c := Cost(prices, u)
	if c == nil || *c != 1+1+0.2+0.5 {
		t.Fatalf("Cost = %v", c)
	}
	if Cost(nil, u) != nil || Cost(prices, provider.Usage{}) != nil {
		t.Fatal("без цен или без расхода стоимость неизвестна")
	}
}
