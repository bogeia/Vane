package biz

import (
	"context"
	"strings"
	"time"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

const (
	SummaryProductRelease = "product_release"
	SummaryPaper          = "paper"
	SummaryTutorial       = "tutorial"
)

// SummaryType identifies only selected, single-topic material with a supported focus.
func SummaryType(item WorkItem) string {
	if !item.Selected || item.Writing == nil || item.Writing.Roundup {
		return ""
	}
	switch item.Writing.Category {
	case taxonomy.CategoryPaper:
		return SummaryPaper
	case taxonomy.CategoryTutorial:
		return SummaryTutorial
	case taxonomy.CategoryProduct:
		if fact := item.Writing.Fact; fact != nil {
			action := strings.ToLower(strings.TrimSpace(fact.Action))
			for _, uncertain := range []string{"未", "不再", "取消", "停止", "暂停", "计划", "预计", "拟", "即将", "将于", "cancel", "stop", "plan", "expect"} {
				if strings.Contains(action, uncertain) {
					return ""
				}
			}
			for _, release := range []string{"发布", "推出", "上线", "更新", "开源", "release", "launch", "introduce", "update", "roll out", "rolls out", "unveil"} {
				if strings.Contains(action, release) {
					return SummaryProductRelease
				}
			}
		}
	}
	return ""
}

func (p *Processor) enhanceSummary(ctx context.Context, item WorkItem) (Writing, error) {
	started := time.Now()
	summary, err := p.writer.EnhanceSummary(ctx, item)
	// Cancellation is not a fallback: no checkpoint or publication may follow it.
	if ctx.Err() != nil {
		return Writing{}, ctx.Err()
	}
	writing := *item.Writing
	outcome := "fallback"
	if err == nil {
		candidate := writing
		candidate.Summary = summary
		if normalized, validationErr := NormalizeWriting(candidate); validationErr == nil {
			writing.Summary = normalized.Summary
			outcome = "success"
		}
	}
	p.log.InfoContext(ctx, "summary enhancement completed", "item_id", item.ID, "summary_type", SummaryType(item), "outcome", outcome, "duration", time.Since(started))
	return writing, nil
}
