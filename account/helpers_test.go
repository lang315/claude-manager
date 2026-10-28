package account

import "time"

// t0 is the tests' clock.
var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

// login is a login whose access token at lapses at expires.
func login(at, rt string, expires time.Time) Login {
	return Login{AccessToken: at, RefreshToken: rt, ExpiresAt: expires.UnixMilli(),
		Scopes: []string{"user:inference", "user:profile"}, SubscriptionType: "max"}
}
