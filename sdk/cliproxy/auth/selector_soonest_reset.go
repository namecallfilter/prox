package auth

import (
	"context"
	"strconv"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// SoonestResetSelector prefers the credential whose longest quota window resets
// soonest, so allowance that is about to expire is spent before allowance that
// lasts for days. Credentials with no upcoming reset on record are picked first,
// in rotation: one response is enough to learn their reset time, and providers
// that never report one keep plain round-robin.
type SoonestResetSelector struct {
	unobserved RoundRobinSelector
}

// Pick selects the available auth with the soonest long-window quota reset.
func (s *SoonestResetSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	now := time.Now()
	available, err := getSelectorAvailableAuths(ctx, auths, provider, model, now)
	if err != nil {
		return nil, err
	}
	available = preferCodexWebsocketAuths(ctx, provider, available)

	var unobserved []*Auth
	var soonest *Auth
	var soonestReset time.Time
	for _, candidate := range available {
		resetAt := longWindowResetAt(candidate.Quota)
		if !resetAt.After(now) {
			unobserved = append(unobserved, candidate)
			continue
		}
		if soonest == nil || resetAt.Before(soonestReset) {
			soonest, soonestReset = candidate, resetAt
		}
	}
	if len(unobserved) > 0 {
		return s.unobserved.Pick(ctx, provider, model, opts, unobserved)
	}
	return soonest, nil
}

// longWindowResetAt returns when the longest quota window in the credential's
// last observed snapshot resets, or the zero time when none was reported.
func longWindowResetAt(quota QuotaState) time.Time {
	if resetAt := unixSignal(quota.Signals["Anthropic-Ratelimit-Unified-7d-Reset"]); !resetAt.IsZero() {
		return resetAt
	}

	// Codex plans differ in which of the two windows is the long one, so the
	// reported window length decides rather than the primary/secondary name.
	var resetAt time.Time
	var longestMinutes float64
	for _, window := range []string{"X-Codex-Primary-", "X-Codex-Secondary-"} {
		minutes, errMinutes := strconv.ParseFloat(quota.Signals[window+"Window-Minutes"], 64)
		if errMinutes != nil || minutes <= longestMinutes {
			continue
		}
		windowReset := unixSignal(quota.Signals[window+"Reset-At"])
		if windowReset.IsZero() {
			seconds, errSeconds := strconv.ParseFloat(quota.Signals[window+"Reset-After-Seconds"], 64)
			if errSeconds != nil || quota.ObservedAt.IsZero() {
				continue
			}
			windowReset = quota.ObservedAt.Add(time.Duration(seconds * float64(time.Second)))
		}
		longestMinutes, resetAt = minutes, windowReset
	}
	return resetAt
}

func unixSignal(raw string) time.Time {
	seconds, errParse := strconv.ParseFloat(raw, 64)
	if errParse != nil || seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(seconds), 0)
}
