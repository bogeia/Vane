package llm

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

//go:embed prompts/discussion-matching.md
var discussionMatchingPrompt string

func (c *Client) MatchDiscussion(ctx context.Context, input biz.DiscussionMatchInput) (biz.DiscussionMatch, error) {
	material, err := discussionMatchMaterial(input)
	if err != nil {
		return biz.DiscussionMatch{}, err
	}
	if len(input.Candidates) == 0 {
		return biz.DiscussionMatch{Decision: biz.DiscussionMatchUnknown}, nil
	}
	body, err := c.evaluateJSON(ctx, material, "discussion_matching", discussionMatchingPrompt, nil)
	if err != nil {
		return biz.DiscussionMatch{}, err
	}
	return parseDiscussionMatch(body, input)
}

func discussionMatchMaterial(input biz.DiscussionMatchInput) (json.RawMessage, error) {
	type itemDTO struct {
		ID            int64     `json:"item_id"`
		URL           string    `json:"url"`
		OriginalTitle string    `json:"original_title"`
		Summary       string    `json:"summary"`
		PublishedAt   time.Time `json:"published_at"`
		Fact          factDTO   `json:"fact"`
	}
	type threadDTO struct {
		ID          int64     `json:"thread_id"`
		Title       string    `json:"title"`
		URL         string    `json:"url"`
		SubmittedAt time.Time `json:"submitted_at"`
	}
	type candidateDTO struct {
		StoryID int64     `json:"story_id"`
		Items   []itemDTO `json:"items"`
	}
	if input.Thread.ID <= 0 || len(input.Candidates) > biz.DiscussionEventLimit {
		return nil, fmt.Errorf("invalid discussion matching input")
	}
	candidates := make([]candidateDTO, 0, len(input.Candidates))
	stories, items := map[int64]bool{}, map[int64]bool{}
	for _, candidate := range input.Candidates {
		if candidate.StoryID <= 0 || stories[candidate.StoryID] || len(candidate.Items) == 0 {
			return nil, fmt.Errorf("invalid discussion matching candidate")
		}
		stories[candidate.StoryID] = true
		projected := candidateDTO{StoryID: candidate.StoryID, Items: make([]itemDTO, 0, len(candidate.Items))}
		for _, item := range candidate.Items {
			if item.ID <= 0 || items[item.ID] {
				return nil, fmt.Errorf("invalid discussion matching item")
			}
			items[item.ID] = true
			fact := item.Fact
			projected.Items = append(projected.Items, itemDTO{item.ID, item.URL, item.OriginalTitle, item.Summary, item.PublishedAt, factDTO{fact.Title, fact.Subject, fact.Action, fact.Object, fact.OccurredAt}})
		}
		candidates = append(candidates, projected)
	}
	material, err := json.Marshal(struct {
		Thread     threadDTO      `json:"thread"`
		Candidates []candidateDTO `json:"candidates"`
	}{Thread: threadDTO{input.Thread.ID, input.Thread.Title, input.Thread.URL, input.Thread.PublishedAt}, Candidates: candidates})
	// Reject oversized batches instead of truncating facts and changing quote identity.
	if err != nil || len(material) > 256*1024 {
		return nil, fmt.Errorf("discussion matching input exceeds limit")
	}
	return material, nil
}

func parseDiscussionMatch(body []byte, input biz.DiscussionMatchInput) (biz.DiscussionMatch, error) {
	var result struct {
		Decision *biz.DiscussionMatchDecision `json:"decision"`
		StoryID  *int64                       `json:"story_id"`
		Evidence *[]struct {
			Role       *string `json:"role"`
			HNField    *string `json:"hn_field"`
			HNQuote    *string `json:"hn_quote"`
			ItemID     *int64  `json:"item_id"`
			EventField *string `json:"event_field"`
			EventQuote *string `json:"event_quote"`
		} `json:"evidence"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Decision == nil || result.StoryID == nil || result.Evidence == nil {
		return biz.DiscussionMatch{}, fmt.Errorf("invalid discussion matching JSON")
	}
	match := biz.DiscussionMatch{Decision: *result.Decision, StoryID: *result.StoryID}
	switch match.Decision {
	case biz.DiscussionMatchUnknown, biz.DiscussionMatchDifferent:
		if match.StoryID != 0 || len(*result.Evidence) != 0 {
			return biz.DiscussionMatch{}, fmt.Errorf("invalid unmatched discussion evidence")
		}
		return match, nil
	case biz.DiscussionMatchSame:
	default:
		return biz.DiscussionMatch{}, fmt.Errorf("invalid discussion matching decision")
	}
	for _, evidence := range *result.Evidence {
		if evidence.Role == nil || evidence.HNField == nil || evidence.HNQuote == nil || evidence.ItemID == nil || evidence.EventField == nil || evidence.EventQuote == nil {
			return biz.DiscussionMatch{}, fmt.Errorf("incomplete discussion matching evidence")
		}
		match.Evidence = append(match.Evidence, biz.DiscussionMatchEvidence{Role: *evidence.Role, HNField: *evidence.HNField, HNQuote: *evidence.HNQuote, ItemID: *evidence.ItemID, EventField: *evidence.EventField, EventQuote: *evidence.EventQuote})
	}
	return biz.ValidateDiscussionMatch(input, match)
}

var _ biz.DiscussionMatcher = (*Client)(nil)
