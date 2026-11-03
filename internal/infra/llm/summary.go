package llm

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bogeia/vane/internal/biz"
)

//go:embed prompts/summary.md
var summaryPrompt string

func (c *Client) EnhanceSummary(ctx context.Context, item biz.WorkItem) (string, error) {
	if item.Writing == nil {
		return "", fmt.Errorf("summary enhancement requires writing")
	}
	summaryType := biz.SummaryType(item)
	if summaryType == "" {
		return "", nil
	}
	body := []rune(item.Body)
	content, err := c.evaluateJSON(ctx, struct {
		Type          string `json:"type"`
		OriginalTitle string `json:"original_title"`
		Body          string `json:"body"`
		Title         string `json:"title"`
		Summary       string `json:"summary"`
		Category      string `json:"category"`
	}{summaryType, item.OriginalTitle, string(body[:min(len(body), 6000)]), item.Writing.Title, item.Writing.Summary, item.Writing.Category}, "summary_enhancement", summaryPrompt, nil)
	if err != nil {
		return "", err
	}
	var result struct {
		Summary *string `json:"summary"`
	}
	if json.Unmarshal(content, &result) != nil || result.Summary == nil {
		return "", fmt.Errorf("invalid summary enhancement JSON")
	}
	if strings.TrimSpace(*result.Summary) == "" {
		return "", nil
	}
	writing := *item.Writing
	writing.Summary = *result.Summary
	normalized, err := biz.NormalizeWriting(writing)
	if err != nil {
		return "", err
	}
	return normalized.Summary, nil
}
