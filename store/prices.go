package store

import "regexp"

// PricesAsOf is the day the price table was read from
// https://platform.claude.com/docs/en/about-claude/pricing.
const PricesAsOf = "2026-10-27"

// rates are USD per million tokens.
type rates struct{ input, write5m, write1h, read, output float64 }

var prices = map[string]rates{
	"claude-fable-5-1":  {10, 12.50, 20, 0.25, 50},
	"claude-fable-5":    {10, 12.50, 20, 1, 50},
	"claude-opus-5-5":   {4, 5, 8, 0.20, 20},
	"claude-opus-5":     {5, 6.25, 10, 0.50, 25},
	"claude-opus-4-8":   {5, 6.25, 10, 0.50, 25},
	"claude-opus-4-7":   {5, 6.25, 10, 0.50, 25},
	"claude-opus-4-6":   {5, 6.25, 10, 0.50, 25},
	"claude-opus-4-5":   {5, 6.25, 10, 0.50, 25},
	"claude-sonnet-5-5": {2, 2.50, 4, 0.10, 10},
	"claude-sonnet-5":   {2, 2.50, 4, 0.20, 10},
	"claude-sonnet-4-6": {3, 3.75, 6, 0.30, 15},
	"claude-sonnet-4-5": {3, 3.75, 6, 0.30, 15},
	"claude-haiku-5-5":  {0.10, 0.125, 0.20, 0.01, 0.50},
	"claude-haiku-4-5":  {1, 1.25, 2, 0.10, 5},
}

// haiku55Long are Claude Haiku 5.5's rates for a prompt over 100,000 tokens.
var haiku55Long = rates{0.50, 0.625, 1, 0.05, 2.50}

// fastModels run fast mode at twice every rate.
var fastModels = map[string]bool{"claude-opus-5-5": true, "claude-opus-5": true, "claude-opus-4-8": true}

// noGeo are the models before Claude 4.6: US-only inference costs them
// nothing more.
var noGeo = map[string]bool{"claude-opus-4-5": true, "claude-sonnet-4-5": true, "claude-haiku-4-5": true}

const webSearch = 10.0 / 1000 // USD a search

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// callTokens are the counts a call is priced on.
type callTokens struct {
	Input, Output, CacheRead, CacheWrite5m, CacheWrite1h, WebSearches int64

	Fast bool // speed "fast"
	US   bool // inference_geo "us"
}

// price is a call's cost at API list prices, in USD, and false for a model
// the table doesn't have.
func price(model string, c callTokens) (float64, bool) {
	m := dateSuffix.ReplaceAllString(model, "")
	r, ok := prices[m]
	if !ok {
		return 0, false
	}
	// Haiku 5.5 prices each request by its prompt: input, cache reads and writes.
	if m == "claude-haiku-5-5" && c.Input+c.CacheRead+c.CacheWrite5m+c.CacheWrite1h > 100_000 {
		r = haiku55Long
	}
	k := 1.0
	if c.Fast && fastModels[m] {
		k *= 2
	}
	if c.US && !noGeo[m] {
		k *= 1.1
	}
	t := float64(c.Input)*r.input + float64(c.Output)*r.output + float64(c.CacheRead)*r.read +
		float64(c.CacheWrite5m)*r.write5m + float64(c.CacheWrite1h)*r.write1h
	return t/1e6*k + float64(c.WebSearches)*webSearch, true
}
