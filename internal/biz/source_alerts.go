package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type SourceAlertFetch struct {
	FetchedAt time.Time
	Success   bool
	Error     string
}
type SourceAlertState struct {
	ID, Name       string
	AlertStartedAt *time.Time
	Records        []SourceAlertFetch
}
type SourceAlertsRepository interface {
	SourcesForAlerts(context.Context) ([]SourceAlertState, error)
	SetSourceAlert(context.Context, string, *time.Time) error
}
type (
	SourceAlertOptions  struct{ Groups []string }
	SourceAlertsUseCase struct {
		repo    SourceAlertsRepository
		sender  MessageSender
		options SourceAlertOptions
		mu      sync.Mutex
	}
)

func NewSourceAlertsUseCase(repo SourceAlertsRepository, sender MessageSender, options SourceAlertOptions) *SourceAlertsUseCase {
	return &SourceAlertsUseCase{repo: repo, sender: sender, options: options}
}

func (u *SourceAlertsUseCase) RunOnce(ctx context.Context) error {
	if len(u.options.Groups) == 0 {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	sources, err := u.repo.SourcesForAlerts(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return errors.Join(failures, err)
		}
		if len(source.Records) == 0 {
			continue
		}
		latest := source.Records[0]
		var start *time.Time
		var message PushMessage
		escape := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "<", "&lt;", ">", "&gt;", "\n", " ", "\r", " ")
		identity := fmt.Sprintf("信源：%s\nID：%s", escape.Replace(source.Name), escape.Replace(source.ID))
		if source.AlertStartedAt != nil {
			if !latest.Success {
				continue
			}
			message = PushMessage{Title: "信源恢复", Content: fmt.Sprintf("%s\n中断时长：%s", identity, formatAlertDuration(latest.FetchedAt.Sub(*source.AlertStartedAt)))}
		} else {
			count := 0
			for _, record := range source.Records {
				if record.Success {
					break
				}
				count++
				start = &record.FetchedAt
			}
			if count < 3 {
				continue
			}
			message = PushMessage{Title: "信源告警", Content: fmt.Sprintf("%s\n连续失败：%d\n首次失败：%s\n最后错误：%s", identity, count, start.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04"), escape.Replace(latest.Error))}
		}
		var sendError error
		for _, group := range u.options.Groups {
			if err := ctx.Err(); err != nil {
				return errors.Join(failures, err)
			}
			sendError = errors.Join(sendError, u.sender.Send(ctx, group, []PushMessage{message}))
		}
		if sendError != nil {
			failures = errors.Join(failures, sendError)
			continue
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(failures, err)
		}
		failures = errors.Join(failures, u.repo.SetSourceAlert(ctx, source.ID, start))
	}
	return failures
}

func formatAlertDuration(duration time.Duration) string {
	minutes := int64(duration / time.Minute)
	switch {
	case minutes < 1:
		return "不到 1 分钟"
	case minutes < 60:
		return fmt.Sprintf("%d 分钟", minutes)
	case minutes < 24*60:
		result := fmt.Sprintf("%d 小时", minutes/60)
		if minutes%60 != 0 {
			result += fmt.Sprintf(" %d 分钟", minutes%60)
		}
		return result
	default:
		result := fmt.Sprintf("%d 天", minutes/(24*60))
		if hours := minutes / 60 % 24; hours != 0 {
			result += fmt.Sprintf(" %d 小时", hours)
		}
		return result
	}
}
