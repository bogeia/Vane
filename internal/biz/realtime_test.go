package biz

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type realtimeRetryRepo struct {
	records []PushDelivery
	target  PushTarget
}

func (r *realtimeRetryRepo) SyncTargets(context.Context, []string, time.Time) error { return nil }
func (r *realtimeRetryRepo) Target(context.Context, string) (PushTarget, error)     { return r.target, nil }
func (r *realtimeRetryRepo) Candidates(context.Context, RealtimeQuery) ([]RealtimeCandidate, error) {
	return nil, nil
}

func (r *realtimeRetryRepo) Deliveries(context.Context, string) ([]PushDelivery, error) {
	return r.records, nil
}

func (r *realtimeRetryRepo) SaveDeliveries(_ context.Context, records []PushDelivery) error {
	r.records = records
	return nil
}

type realtimeRetrySender struct{ sent int }

func (s *realtimeRetrySender) Send(context.Context, string, []PushMessage) error {
	s.sent++
	return nil
}

func (s *realtimeRetrySender) RealtimeMessages(_ string, items []Item, _ bool, _ string) ([]RealtimePart, error) {
	return []RealtimePart{{ItemIDs: []string{items[0].ID}}}, nil
}

func TestRealtimeRetryRechecksEligibility(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name     string
		age      time.Duration
		words    [][]string
		selected bool
		wantSent int
		roundup  bool
		scope    string
	}{
		{"expired", 12*time.Hour + time.Minute, nil, true, 0, false, "selected"},
		{"changed words", time.Hour, [][]string{{"GPT"}}, true, 0, false, "selected"},
		{"no longer selected", time.Hour, nil, false, 0, false, "selected"},
		{"still eligible", time.Hour, nil, true, 1, false, "selected"},
		{"selected roundup", time.Hour, nil, true, 0, true, "selected"},
		{"all roundup", time.Hour, [][]string{{"Claude"}}, false, 0, true, "all"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &realtimeRetryRepo{target: PushTarget{FirstSeenAt: now.Add(-time.Hour)}, records: []PushDelivery{{Target: "readers", StoryID: "1", Item: Item{ID: "1", Title: "Claude", Selected: tt.selected, Roundup: tt.roundup, PublishedAt: now.Add(-tt.age)}, ProcessedAt: now.Add(-time.Minute), Status: DeliveryFailed, Attempts: 1, NextAttemptAt: now.Add(-time.Minute)}}}
			sender := &realtimeRetrySender{}
			u := NewRealtimeUseCase(repo, sender, RealtimeOptions{Groups: []RealtimeGroup{{Name: "readers", Enabled: true, Scope: tt.scope, Words: tt.words}}}, func() time.Time { return now }, slog.Default())
			require.NoError(t, u.RunOnce(t.Context()))
			assert.Equal(t, tt.wantSent, sender.sent)
			if tt.wantSent == 0 {
				require.Len(t, repo.records, 1)
				assert.Equal(t, DeliveryAbandoned, repo.records[0].Status)
				assert.Equal(t, 1, repo.records[0].Attempts)
				assert.Equal(t, "push no longer eligible", repo.records[0].LastError)
			}
			require.NoError(t, u.RunOnce(t.Context()))
			assert.Equal(t, tt.wantSent, sender.sent, "terminal delivery retried")
		})
	}
}
