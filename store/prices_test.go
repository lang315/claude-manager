package store

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPrice(t *testing.T) {
	M := int64(1_000_000)
	for _, c := range []struct {
		name  string
		model string
		tok   callTokens
		want  float64
	}{
		{"each rate", "claude-opus-5-5", callTokens{Input: M, Output: M, CacheRead: M, CacheWrite5m: M, CacheWrite1h: M}, 4 + 20 + 0.20 + 5 + 8},
		{"date suffix", "claude-haiku-4-5-20251001", callTokens{Input: M, Output: M}, 1 + 5},
		{"haiku 5.5 short prompt", "claude-haiku-5-5", callTokens{Input: 100_000, Output: M}, 0.01 + 0.50},
		{"haiku 5.5 long prompt counts cache", "claude-haiku-5-5", callTokens{Input: 50_000, CacheRead: 60_000, Output: M}, 0.025 + 0.003 + 2.50},
		{"fast doubles", "claude-opus-5-5", callTokens{Input: M, Fast: true}, 8},
		{"fast only on fast models", "claude-sonnet-5-5", callTokens{Input: M, Fast: true}, 2},
		{"us adds a tenth", "claude-sonnet-5-5", callTokens{Input: M, US: true}, 2.2},
		{"us and fast stack", "claude-opus-5-5", callTokens{Input: M, Fast: true, US: true}, 8.8},
		{"us not on 4.5", "claude-sonnet-4-5", callTokens{Input: M, US: true}, 3},
		{"fable 5.1 cache read", "claude-fable-5-1", callTokens{CacheRead: M}, 0.25},
		{"web search", "claude-sonnet-5", callTokens{WebSearches: 1000}, 10},
	} {
		got, ok := price(c.model, c.tok)
		if !ok || !near(got, c.want) {
			t.Errorf("%s: price = %v, %v; want %v", c.name, got, ok, c.want)
		}
	}
	if _, ok := price("claude-future-9", callTokens{Input: 1}); ok {
		t.Error("an unknown model has a price")
	}
}
