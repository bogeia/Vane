package llm

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/bogeia/vane/internal/biz"
)

//go:embed prompts/discussions.md
var discussionPrompt string

func (c *Client) SummarizeDiscussion(ctx context.Context, input biz.DiscussionInput) ([]biz.DiscussionPoint, error) {
	type commentDTO struct {
		ID   int64  `json:"comment_id"`
		Text string `json:"text"`
	}
	comments := make([]commentDTO, 0, len(input.Comments))
	allowed := map[int64]bool{}
	for _, comment := range input.Comments {
		comments = append(comments, commentDTO{comment.ID, comment.Text})
		allowed[comment.ID] = true
	}
	content, err := c.evaluateJSON(ctx, struct {
		Title    string       `json:"thread_title"`
		Comments []commentDTO `json:"comments"`
	}{input.Thread.Title, comments}, "discussion_summary", discussionPrompt, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Points *[]struct {
			Text      *string `json:"text"`
			CommentID *int64  `json:"comment_id"`
		} `json:"points"`
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Points == nil || len(*result.Points) > 3 {
		return nil, fmt.Errorf("invalid discussion summary JSON")
	}
	points := make([]biz.DiscussionPoint, 0, len(*result.Points))
	for _, point := range *result.Points {
		if point.Text == nil || point.CommentID == nil || !allowed[*point.CommentID] {
			return nil, fmt.Errorf("invalid discussion summary reference")
		}
		text := strings.TrimSpace(*point.Text)
		if text == "" || utf8.RuneCountInString(text) > 240 {
			return nil, fmt.Errorf("invalid discussion summary text")
		}
		points = append(points, biz.DiscussionPoint{Text: text, CommentID: *point.CommentID})
	}
	return points, nil
}

var _ biz.DiscussionSummarizer = (*Client)(nil)
