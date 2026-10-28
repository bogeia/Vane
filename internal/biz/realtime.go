package biz

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bogeia/vane/internal/biz/taxonomy"
	"github.com/bogeia/vane/internal/biz/watchwords"
)

const (
	DeliverySent      = "sent"
	DeliveryFailed    = "failed"
	DeliveryAbandoned = "abandoned"
)

type PushTarget struct {
	Name        string
	FirstSeenAt time.Time
}
type RealtimeCandidate struct {
	Item        Item
	ProcessedAt time.Time
}
type PushDelivery struct {
	Target, StoryID      string
	Item                 Item
	Status               string
	Attempts             int
	NextAttemptAt        time.Time
	LastError            string
	CreatedAt, UpdatedAt time.Time
	ProcessedAt          time.Time
}
type (
	RealtimeQuery      struct{ ProcessedSince, PublishedSince, Until time.Time }
	RealtimeRepository interface {
		SyncTargets(context.Context, []string, time.Time) error
		Target(context.Context, string) (PushTarget, error)
		Candidates(context.Context, RealtimeQuery) ([]RealtimeCandidate, error)
		Deliveries(context.Context, string) ([]PushDelivery, error)
		SaveDeliveries(context.Context, []PushDelivery) error
	}
)

type RealtimeGroup struct {
	Name    string
	Enabled bool
	Scope   string
	Topics  []string
	Words   [][]string
}
type (
	RealtimeOptions struct{ Groups []RealtimeGroup }
	RealtimePart    struct {
		Message PushMessage
		ItemIDs []string
	}
)

type RealtimeSender interface {
	MessageSender
	RealtimeMessages(string, []Item, bool, string) ([]RealtimePart, error)
}
type RealtimeUseCase struct {
	repo    RealtimeRepository
	sender  RealtimeSender
	options RealtimeOptions
	now     func() time.Time
	log     *slog.Logger
	mu      sync.Mutex
}

func NewRealtimeUseCase(repo RealtimeRepository, sender RealtimeSender, options RealtimeOptions, now func() time.Time, log *slog.Logger) *RealtimeUseCase {
	return &RealtimeUseCase{repo: repo, sender: sender, options: options, now: now, log: log}
}

func (u *RealtimeUseCase) RunOnce(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := u.now()
	var failures error
	for _, group := range u.options.Groups {
		if !group.Enabled {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := u.runGroup(ctx, group, now); err != nil {
			failures = errors.Join(failures, err)
		}
	}
	return failures
}

func (u *RealtimeUseCase) runGroup(ctx context.Context, group RealtimeGroup, now time.Time) error {
	target, err := u.repo.Target(ctx, group.Name)
	if err != nil {
		return err
	}
	records, err := u.repo.Deliveries(ctx, group.Name)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var queue []PushDelivery
	for _, record := range records {
		seen[record.StoryID] = true
		if record.Status == DeliveryFailed && !record.NextAttemptAt.After(now) {
			_, eligible := realtimeReason(group, record.Item)
			if !eligible || record.Item.PublishedAt.Before(now.Add(-12*time.Hour)) || record.Item.PublishedAt.After(now) || record.ProcessedAt.Before(target.FirstSeenAt) {
				record.Status = DeliveryAbandoned
				record.LastError = "push no longer eligible"
				record.NextAttemptAt = time.Time{}
				record.UpdatedAt = now
				if err := u.repo.SaveDeliveries(ctx, []PushDelivery{record}); err != nil {
					return err
				}
				continue
			}
			queue = append(queue, record)
		}
	}
	candidates, err := u.repo.Candidates(ctx, RealtimeQuery{ProcessedSince: target.FirstSeenAt, PublishedSince: now.Add(-12 * time.Hour), Until: now})
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		item := candidate.Item
		if _, eligible := realtimeReason(group, item); !eligible || item.StoryID == "" || seen[item.StoryID] {
			continue
		}
		seen[item.StoryID] = true
		queue = append(queue, PushDelivery{Target: group.Name, StoryID: item.StoryID, Item: CompleteItem(item), CreatedAt: now, ProcessedAt: candidate.ProcessedAt})
	}
	slices.SortStableFunc(queue, func(a, b PushDelivery) int {
		if order := a.ProcessedAt.Compare(b.ProcessedAt); order != 0 {
			return order
		}
		return cmp.Compare(a.Item.ID, b.Item.ID)
	})
	var failures error
	for index := 0; index < len(queue); {
		end := index + 1
		list := index >= 5
		if list {
			end = len(queue)
		}
		batch := queue[index:end]
		if err := ctx.Err(); err != nil {
			return err
		}
		items := make([]Item, len(batch))
		for i := range batch {
			items[i] = batch[i].Item
		}
		reason, _ := realtimeReason(group, items[0])
		parts, renderErr := u.sender.RealtimeMessages(group.Name, items, list, reason)
		if renderErr != nil {
			if err := u.recordBatch(ctx, group.Name, batch, now, renderErr); err != nil {
				return err
			}
			failures = errors.Join(failures, renderErr)
		} else {
			for _, part := range parts {
				var delivered []PushDelivery
				for _, delivery := range batch {
					if slices.Contains(part.ItemIDs, delivery.Item.ID) {
						delivered = append(delivered, delivery)
					}
				}
				sendErr := u.sender.Send(ctx, group.Name, []PushMessage{part.Message})
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err := u.recordBatch(ctx, group.Name, delivered, now, sendErr); err != nil {
					return err
				}
				failures = errors.Join(failures, sendErr)
			}
		}
		index = end
	}
	return failures
}

func (u *RealtimeUseCase) recordBatch(ctx context.Context, name string, batch []PushDelivery, now time.Time, sendErr error) error {
	for i := range batch {
		delivery := &batch[i]
		delivery.Attempts++
		delivery.UpdatedAt = now
		delivery.LastError = ""
		delivery.Status = DeliverySent
		delivery.NextAttemptAt = time.Time{}
		if sendErr != nil {
			delivery.Status = DeliveryFailed
			delivery.LastError = "push request failed"
			delivery.NextAttemptAt = now.Add(time.Minute)
			if delivery.Attempts >= 4 {
				delivery.Status = DeliveryAbandoned
				delivery.NextAttemptAt = time.Time{}
				u.log.ErrorContext(ctx, "realtime push abandoned", "group", name, "status", DeliveryAbandoned)
			}
		}
	}
	return u.repo.SaveDeliveries(ctx, batch)
}

func realtimeReason(group RealtimeGroup, item Item) (string, bool) {
	if item.Roundup {
		return "", false
	}
	if group.Scope == taxonomy.ScopeSelected && !item.Selected {
		return "", false
	}
	if group.Scope != taxonomy.ScopeSelected && group.Scope != taxonomy.ScopeAll {
		return "", false
	}
	if group.Scope == taxonomy.ScopeAll && len(group.Topics) == 0 && len(group.Words) == 0 {
		return "", false
	}
	var reasons []string
	if len(group.Topics) > 0 {
		var names []string
		for _, topic := range taxonomy.Topics {
			if slices.Contains(group.Topics, topic.Slug) && slices.Contains(item.Topics, topic.Slug) {
				names = append(names, topic.Name)
			}
		}
		if len(names) == 0 {
			return "", false
		}
		reasons = append(reasons, "主题："+strings.Join(names, "、"))
	}
	if len(group.Words) > 0 {
		matched, hits := watchwords.MatchWords(group.Words, item.Title, item.OriginalTitle)
		if !matched {
			return "", false
		}
		reasons = append(reasons, "关注词："+strings.Join(hits, "、"))
	}
	if len(reasons) == 0 {
		return "精选", true
	}
	return strings.Join(reasons, "；"), true
}
