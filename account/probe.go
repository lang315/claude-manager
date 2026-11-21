package account

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// claude-swap's header probe (oauth.py request_usage_via_probe): the usage
// endpoint doesn't answer a setup-token, but every /v1/messages answer
// carries the account's unified 5h and 7d utilization; a model's own
// weekly window comes only on that model's answers.
const (
	probeModel   = "claude-haiku-5-5"
	probeVersion = "2023-06-01"
	probeSystem  = "You are Claude Code, Anthropic's official CLI for Claude." // OAuth inference needs Claude Code's prompt
)

// probeScoped is each model probed for its own weekly window: its header
// bucket and the name the usage endpoint gives that window.
var probeScoped = []struct{ model, bucket, name string }{{"claude-fable-5-1", "7d_oi", "Fable"}}

// Probe reads token's limits from the rate-limit headers of 1-token
// requests: a Haiku one for the 5h and 7d windows, then one per scoped
// model. A 429 refusing the account reads as its five-hour window at 100%
// until the refusal ends; a refused scoped model, as that model's window
// at 100%.
func (a *API) Probe(ctx context.Context, token string, now time.Time) ([]Limit, error) {
	h, status, err := a.probe(ctx, token, probeModel)
	if err != nil {
		return nil, err
	}
	if refused(h, status) {
		return []Limit{{Kind: "session", Percent: 100, ResetsAt: refusalEnds(h, "", now)}}, nil
	}
	if status != http.StatusOK {
		return nil, probeError(status, h)
	}
	var out []Limit
	for _, w := range []struct{ bucket, kind string }{{"5h", "session"}, {"7d", "weekly_all"}} {
		if l, ok := unified(h, w.bucket, w.kind, ""); ok {
			out = append(out, l)
		}
	}
	for _, s := range probeScoped {
		h, status, err := a.probe(ctx, token, s.model)
		switch {
		case err != nil:
		case refused(h, status):
			out = append(out, Limit{Kind: "weekly_scoped", Scope: s.name, Percent: 100, ResetsAt: refusalEnds(h, s.bucket, now)})
		case status == http.StatusOK:
			if l, ok := unified(h, s.bucket, "weekly_scoped", s.name); ok {
				out = append(out, l)
			}
		}
	}
	if len(out) == 0 {
		return nil, errNoLimits
	}
	return out, nil
}

func (a *API) probe(ctx context.Context, token, model string) (http.Header, int, error) {
	body, _ := json.Marshal(map[string]any{"model": model, "max_tokens": 1, "system": probeSystem,
		"messages": []map[string]string{{"role": "user", "content": "hi"}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("anthropic-version", probeVersion)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", a.Agent)
	start := time.Now()
	resp, err := a.Client.Do(req)
	a.debug(req, resp, start)
	if err != nil {
		return nil, 0, unreachable(err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	return resp.Header, resp.StatusCode, nil
}

func refused(h http.Header, status int) bool {
	return status == http.StatusTooManyRequests && h.Get("anthropic-ratelimit-unified-status") == "rejected"
}

// refusalEnds is when a refusal lifts: Retry-After from now, else the
// bucket's reset, else the unified reset.
func refusalEnds(h http.Header, bucket string, now time.Time) time.Time {
	if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s > 0 {
		return now.Add(time.Duration(s) * time.Second)
	}
	if bucket != "" {
		if t := epoch(h.Get("anthropic-ratelimit-unified-" + bucket + "-reset")); !t.IsZero() {
			return t
		}
	}
	return epoch(h.Get("anthropic-ratelimit-unified-reset"))
}

func probeError(status int, h http.Header) error {
	switch status {
	case http.StatusTooManyRequests:
		return &RateLimited{Wait: retryAfter(h.Get("Retry-After"))}
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	}
	return refusal(status, nil)
}

// unified reads one bucket of the unified headers: utilization a 0–1
// fraction, reset epoch seconds.
func unified(h http.Header, bucket, kind, scope string) (Limit, bool) {
	base := "anthropic-ratelimit-unified-" + bucket
	f, err := strconv.ParseFloat(h.Get(base+"-utilization"), 64)
	if err != nil || f < 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return Limit{}, false
	}
	return Limit{Kind: kind, Scope: scope, Percent: math.Min(math.Round(f*10000)/100, 100), ResetsAt: epoch(h.Get(base + "-reset"))}, true
}

func epoch(s string) time.Time {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 || math.IsInf(f, 0) {
		return time.Time{}
	}
	return time.Unix(int64(f), 0).UTC()
}
