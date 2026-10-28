package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bogeia/vane/internal/biz"
)

type AgentReportsUseCase interface {
	Get(context.Context, string) (biz.Report, error)
	List(context.Context) ([]biz.ReportSummary, error)
}

type AgentDailyInput struct {
	Date string `json:"date,omitempty" jsonschema:"日报日期，格式 YYYY-MM-DD；省略时返回最新一期"`
}

func (a *AgentService) Daily(ctx context.Context, input AgentDailyInput, transport AgentTransport) AgentAnswer {
	date := input.Date
	var recent []biz.ReportSummary
	if date == "" {
		var err error
		recent, err = a.reports.List(ctx)
		if err != nil {
			return a.unavailable(ctx, err)
		}
		if len(recent) == 0 {
			return a.noDaily()
		}
		date = recent[0].Date
	}
	start, end, err := biz.ReportWindow(date)
	if err != nil {
		if input.Date == "" {
			return a.unavailable(ctx, err)
		}
		return a.invalidParameter("date", 0, transport)
	}
	report, err := a.reports.Get(ctx, date)
	if errors.Is(err, biz.ErrReportNotFound) {
		if recent == nil {
			recent, err = a.reports.List(ctx)
			if err != nil {
				return a.unavailable(ctx, err)
			}
		}
		if len(recent) == 0 {
			return a.noDaily()
		}
		dates := make([]string, 0, min(5, len(recent)))
		for _, summary := range recent[:min(5, len(recent))] {
			dates = append(dates, summary.Date)
		}
		return agentAnswer(http.StatusNotFound, a.site.Name+" 日报 · "+date, "没有这一期日报。最近 5 期："+strings.Join(dates, "、")+"。", "", a.site.Name,
			"如实告诉用户没有这一期，不要拿别的日期冒充；用户需要时可以改看上面列出的某一期。")
	}
	if err != nil {
		return a.unavailable(ctx, err)
	}
	weekdays := [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	title := a.site.Name + " 日报 · " + date + "（" + weekdays[end.Weekday()] + "）"
	description := "收录北京时间 " + a.timeText(start) + " 至 " + a.timeText(end) + " 的动态，每天 08:00 出刊。日报页：" + a.site.PublicURL + "/reports/" + date
	external := a.dailyMarkdown(report)
	if external == "" {
		description += "\n\n这一期是空刊，没有收录条目。"
	}
	hint := "要其它日期的日报，请求 " + a.site.PublicURL + "/api/agent/daily/YYYY-MM-DD（真实日期）；没有就如实说，不要换一天冒充。"
	if transport == AgentMCP {
		hint = "要其它日期的日报，传 date=YYYY-MM-DD（真实日期）；没有就如实说，不要换一天冒充。"
	}
	return agentAnswer(http.StatusOK, title, description, external, a.site.Name,
		"先讲头条，再按栏目挑重点；用户要全文再全部列出。", "日报是每天 08:00 发布的固定成品，不等于“过去 24 小时”的滚动列表。", hint, agentTechnicalHint)
}

func (a *AgentService) noDaily() AgentAnswer {
	return agentAnswer(http.StatusNotFound, a.site.Name+" 日报", "还没有出过日报。", "", a.site.Name, "如实告诉用户还没有出过日报，不要拿其它内容冒充。")
}

func (a *AgentService) dailyItem(item biz.Item) string {
	return "[" + agentText(item.Title) + "](" + a.site.PublicURL + "/items/" + item.ID + ") · " + agentText(item.Source.Name)
}

func (a *AgentService) dailyMarkdown(report biz.Report) string {
	parts := make([]string, 0)
	if report.Headline != nil {
		entry := "头条：" + a.dailyItem(*report.Headline)
		if report.Headline.Summary != "" {
			entry += "\n   " + agentText(report.Headline.Summary)
		}
		parts = append(parts, entry)
	}
	sections := []biz.ReportSection{{Category: "看点", Items: report.Highlights}}
	sections = append(sections, report.Sections...)
	for _, section := range sections {
		if len(section.Items) == 0 {
			continue
		}
		lines := []string{"【" + agentText(section.Category) + "】"}
		for index, item := range section.Items {
			entry := fmt.Sprintf("%d. %s", index+1, a.dailyItem(item))
			if item.StoryOthers > 0 {
				entry += fmt.Sprintf(" · 另外 %d 家也报道了", item.StoryOthers)
			}
			if item.Summary != "" {
				entry += "\n   " + agentText(item.Summary)
			}
			lines = append(lines, entry)
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	if len(report.Briefs) > 0 {
		lines := []string{"【快讯】"}
		for _, item := range report.Briefs {
			lines = append(lines, "- "+a.timeText(item.PublishedAt)+" · "+a.dailyItem(item))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	return strings.Join(parts, "\n\n")
}
