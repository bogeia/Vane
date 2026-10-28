package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type alertRepoStub struct {
	sources []SourceAlertState
	writes  int
}

func (r *alertRepoStub) SourcesForAlerts(context.Context) ([]SourceAlertState, error) {
	return r.sources, nil
}

func (r *alertRepoStub) SetSourceAlert(_ context.Context, id string, start *time.Time) error {
	r.writes++
	r.sources[0].AlertStartedAt = start
	return nil
}

type alertSenderStub struct {
	calls    []string
	messages []PushMessage
	fail     bool
}

func (s *alertSenderStub) Send(_ context.Context, name string, messages []PushMessage) error {
	s.calls = append(s.calls, name)
	s.messages = append(s.messages, messages...)
	if s.fail {
		return errors.New("failed")
	}
	return nil
}

func TestSourceAlertsFailureRecoveryAndRetry(t *testing.T) {
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	repo := &alertRepoStub{sources: []SourceAlertState{{ID: "official", Name: "官方", Records: []SourceAlertFetch{{FetchedAt: now, Error: "request failed"}, {FetchedAt: now.Add(-time.Minute), Error: "request failed"}}}}}
	sender := &alertSenderStub{}
	uc := NewSourceAlertsUseCase(repo, sender, SourceAlertOptions{Groups: []string{"ops", "backup"}})
	run := func() {
		t.Helper()
		require.NoError(t, uc.RunOnce(t.Context()))
	}
	run()
	assert.Empty(t, sender.calls, "two failures should not alert")
	repo.sources[0].Records = append(repo.sources[0].Records, SourceAlertFetch{FetchedAt: now.Add(-2 * time.Minute), Error: "request failed"})
	sender.fail = true
	assert.Error(t, uc.RunOnce(t.Context()))
	assert.Zero(t, repo.writes, "failed send must not save alert")
	sender.fail = false
	sender.calls = nil
	run()
	assert.Len(t, sender.calls, 2)
	assert.Equal(t, 1, repo.writes)
	require.NotEmpty(t, sender.messages)
	assert.Contains(t, sender.messages[len(sender.messages)-1].Content, "连续失败：3")
	assert.Contains(t, sender.messages[len(sender.messages)-1].Content, "首次失败：2026-10-23 15:58")
	run()
	assert.Len(t, sender.calls, 2, "repeated open alert")
	repo.sources[0].Records = append([]SourceAlertFetch{{FetchedAt: now.Add(time.Minute), Success: true}}, repo.sources[0].Records...)
	run()
	assert.Nil(t, repo.sources[0].AlertStartedAt)
	require.NotEmpty(t, sender.messages)
	assert.Contains(t, sender.messages[len(sender.messages)-1].Content, "中断时长：3 分钟")
}

func TestSourceAlertsCancellationAndNoGroups(t *testing.T) {
	repo := &alertRepoStub{}
	sender := &alertSenderStub{}
	uc := NewSourceAlertsUseCase(repo, sender, SourceAlertOptions{})
	require.NoError(t, uc.RunOnce(t.Context()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	uc = NewSourceAlertsUseCase(repo, sender, SourceAlertOptions{Groups: []string{"ops"}})
	assert.ErrorIs(t, uc.RunOnce(ctx), context.Canceled)
	assert.Zero(t, repo.writes)
	assert.Empty(t, sender.calls)
}

func TestSourceAlertsRecoveryDurationBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"under minute", 59 * time.Second, "不到 1 分钟"},
		{"one minute", time.Minute, "1 分钟"},
		{"59 minutes", 59*time.Minute + 59*time.Second, "59 分钟"},
		{"two hours", 2 * time.Hour, "2 小时"},
		{"61 minutes", 61 * time.Minute, "1 小时 1 分钟"},
		{"25 hours", 25*time.Hour + 59*time.Minute, "1 天 1 小时"},
		{"three days", 72 * time.Hour, "3 天"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
			repo := &alertRepoStub{sources: []SourceAlertState{{ID: "official", Name: "官方", AlertStartedAt: &start, Records: []SourceAlertFetch{{FetchedAt: start.Add(tt.duration), Success: true}}}}}
			sender := &alertSenderStub{}
			require.NoError(t, NewSourceAlertsUseCase(repo, sender, SourceAlertOptions{Groups: []string{"ops"}}).RunOnce(t.Context()))
			require.Len(t, sender.messages, 1)
			assert.Equal(t, "信源：官方\nID：official\n中断时长："+tt.want, sender.messages[0].Content)
		})
	}
}
