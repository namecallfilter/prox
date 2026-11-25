package auth

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func claudeWeeklyReset(resetAt time.Time) QuotaState {
	return QuotaState{Signals: map[string]string{
		"Anthropic-Ratelimit-Unified-5h-Reset": strconv.FormatInt(resetAt.Add(-200*time.Hour).Unix(), 10),
		"Anthropic-Ratelimit-Unified-7d-Reset": strconv.FormatInt(resetAt.Unix(), 10),
	}}
}

func pickSoonestResetID(t *testing.T, selector *SoonestResetSelector, auths []*Auth) string {
	t.Helper()
	got, err := selector.Pick(context.Background(), "claude", "", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	return got.ID
}

func TestSoonestResetSelectorPick_PrefersSoonestWeeklyReset(t *testing.T) {
	t.Parallel()

	now := time.Now()
	selector := &SoonestResetSelector{}
	auths := []*Auth{
		{ID: "a", Quota: claudeWeeklyReset(now.Add(96 * time.Hour))},
		{ID: "b", Quota: claudeWeeklyReset(now.Add(20 * time.Hour))},
		{ID: "c", Quota: claudeWeeklyReset(now.Add(48 * time.Hour))},
	}

	for i := range 3 {
		if got := pickSoonestResetID(t, selector, auths); got != "b" {
			t.Fatalf("Pick() #%d auth.ID = %q, want %q", i, got, "b")
		}
	}

	auths[1].Unavailable = true
	auths[1].NextRetryAfter = now.Add(time.Hour)
	if got := pickSoonestResetID(t, selector, auths); got != "c" {
		t.Fatalf("Pick() with soonest auth cooling down = %q, want %q", got, "c")
	}
}

func TestSoonestResetSelectorPick_UnobservedAuthsGoFirstInRotation(t *testing.T) {
	t.Parallel()

	now := time.Now()
	selector := &SoonestResetSelector{}
	auths := []*Auth{
		{ID: "a", Quota: claudeWeeklyReset(now.Add(time.Hour))},
		{ID: "b"},
		{ID: "c", Quota: claudeWeeklyReset(now.Add(-time.Hour))},
	}

	for i, want := range []string{"b", "c", "b"} {
		if got := pickSoonestResetID(t, selector, auths); got != want {
			t.Fatalf("Pick() #%d auth.ID = %q, want %q", i, got, want)
		}
	}

	auths[1].Quota = claudeWeeklyReset(now.Add(72 * time.Hour))
	auths[2].Quota = claudeWeeklyReset(now.Add(168 * time.Hour))
	if got := pickSoonestResetID(t, selector, auths); got != "a" {
		t.Fatalf("Pick() after every auth was observed = %q, want %q", got, "a")
	}
}

func TestSoonestResetSelectorPick_CodexUsesLongestWindow(t *testing.T) {
	t.Parallel()

	now := time.Now()
	observedAt := now.Add(-time.Minute)
	unix := func(at time.Time) string { return strconv.FormatInt(at.Unix(), 10) }
	selector := &SoonestResetSelector{}
	auths := []*Auth{
		{ID: "a", Quota: QuotaState{ObservedAt: observedAt, Signals: map[string]string{
			"X-Codex-Primary-Window-Minutes":        "300",
			"X-Codex-Primary-Reset-After-Seconds":   "600",
			"X-Codex-Secondary-Window-Minutes":      "10080",
			"X-Codex-Secondary-Reset-After-Seconds": "345600",
		}}},
		{ID: "b", Quota: QuotaState{ObservedAt: observedAt, Signals: map[string]string{
			"X-Codex-Primary-Window-Minutes": "10080",
			"X-Codex-Primary-Reset-At":       unix(now.Add(24 * time.Hour)),
		}}},
		{ID: "c", Quota: QuotaState{ObservedAt: observedAt, Signals: map[string]string{
			"X-Codex-Primary-Window-Minutes":   "300",
			"X-Codex-Primary-Reset-At":         unix(now.Add(10 * time.Minute)),
			"X-Codex-Secondary-Window-Minutes": "10080",
			"X-Codex-Secondary-Reset-At":       unix(now.Add(48 * time.Hour)),
		}}},
	}

	if got := pickSoonestResetID(t, selector, auths); got != "b" {
		t.Fatalf("Pick() auth.ID = %q, want %q", got, "b")
	}
}

func TestManagerSoonestResetSelection(t *testing.T) {
	for _, affinity := range []bool{false, true} {
		t.Run(fmt.Sprintf("affinity=%t", affinity), func(t *testing.T) {
			var selector Selector = &SoonestResetSelector{}
			if affinity {
				sticky := NewSessionAffinitySelector(selector)
				t.Cleanup(sticky.Stop)
				selector = sticky
			}
			manager := NewManager(nil, selector, nil)
			const provider, model = "soonest-reset-provider", "soonest-reset-model"
			manager.RegisterExecutor(schedulerTestExecutor{provider: provider})
			now := time.Now()
			wantID := t.Name() + "-b"
			for suffix, resetIn := range map[string]time.Duration{"a": 96 * time.Hour, "b": 20 * time.Hour, "c": 48 * time.Hour} {
				auth := &Auth{ID: t.Name() + "-" + suffix, Provider: provider, Status: StatusActive, Quota: claudeWeeklyReset(now.Add(resetIn))}
				registry.GetGlobalRegistry().RegisterClient(auth.ID, provider, []*registry.ModelInfo{{ID: model}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
				if _, errRegister := manager.Register(WithSkipPersist(t.Context()), auth); errRegister != nil {
					t.Fatal(errRegister)
				}
			}

			for i := range 4 {
				opts := cliproxyexecutor.Options{Headers: http.Header{"Session_id": {fmt.Sprintf("session-%d", i)}}}
				picked, _, errPick := manager.pickNext(t.Context(), provider, model, opts, nil)
				if errPick != nil {
					t.Fatalf("pickNext() #%d error = %v", i, errPick)
				}
				if picked.ID != wantID {
					t.Fatalf("pickNext() #%d auth.ID = %q, want %q", i, picked.ID, wantID)
				}
			}
		})
	}
}
