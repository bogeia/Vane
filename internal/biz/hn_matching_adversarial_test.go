package biz

import "testing"

func TestHNMatchingRejectsIncompleteOccurrenceEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, hnTitle, eventTitle, factObject, hnAction, eventAction, objectQuote, hnAnchor, eventAnchor string
		occurredAt                                                                                       string
	}{
		{name: "letter prefixed model version", hnTitle: "OpenAI releases DeepSeek-V3 in 2026", eventTitle: "OpenAI releases DeepSeek-V2 in 2026", factObject: "DeepSeek-V2", hnAction: "releases", eventAction: "发布", objectQuote: "DeepSeek", hnAnchor: "2026", eventAnchor: "2026"},
		{name: "mini versus turbo", hnTitle: "OpenAI releases GPT-4.1 Mini", eventTitle: "OpenAI releases GPT-4.1 Turbo", factObject: "GPT-4.1 Turbo", hnAction: "releases", eventAction: "发布", objectQuote: "GPT-4.1", hnAnchor: "GPT-4.1", eventAnchor: "GPT-4.1"},
		{name: "omitted model size", hnTitle: "OpenAI releases GPT-4.1", eventTitle: "OpenAI releases GPT-4.1 70B", factObject: "GPT-4.1 70B", hnAction: "releases", eventAction: "发布", objectQuote: "GPT-4.1", hnAnchor: "GPT-4.1", eventAnchor: "GPT-4.1"},
		{name: "seven versus seventy billion", hnTitle: "OpenAI releases GPT-4.1 7B", eventTitle: "OpenAI releases GPT-4.1 70B", factObject: "GPT-4.1 70B", hnAction: "releases", eventAction: "发布", objectQuote: "GPT-4.1", hnAnchor: "GPT-4.1", eventAnchor: "GPT-4.1"},
		{name: "beta versus release", hnTitle: "OpenAI releases GPT-4.1 beta", eventTitle: "OpenAI releases GPT-4.1", factObject: "GPT-4.1", hnAction: "releases", eventAction: "发布", objectQuote: "GPT-4.1", hnAnchor: "GPT-4.1", eventAnchor: "GPT-4.1"},
		{name: "old release resubmitted", hnTitle: "OpenAI releases GPT-4.1 (2025)", eventTitle: "OpenAI releases GPT-4.1 in 2026", factObject: "GPT-4.1", hnAction: "releases", eventAction: "发布", objectQuote: "GPT-4.1", hnAnchor: "GPT-4.1", eventAnchor: "GPT-4.1", occurredAt: "2026-11-03"},
		{name: "benchmark is not release", hnTitle: "OpenAI benchmarks GPT-4.1", eventTitle: "OpenAI releases GPT-4.1", factObject: "GPT-4.1", hnAction: "benchmarks", eventAction: "发布", objectQuote: "GPT-4.1", hnAnchor: "GPT-4.1", eventAnchor: "GPT-4.1"},
		{name: "common year is not occurrence anchor", hnTitle: "OpenAI releases Widget in 2026", eventTitle: "OpenAI releases Widget in 2026", factObject: "Widget", hnAction: "releases", eventAction: "发布", objectQuote: "Widget", hnAnchor: "2026", eventAnchor: "2026"},
		{name: "partial version is not full model identifier", hnTitle: "OpenAI releases GPT-4.1", eventTitle: "OpenAI releases GPT-4.1", factObject: "GPT-4.1", hnAction: "releases", eventAction: "发布", objectQuote: "GPT-4.1", hnAnchor: "4.1", eventAnchor: "4.1"},
		{name: "company is not occurrence anchor", hnTitle: "OpenAI releases Widget", eventTitle: "OpenAI releases Widget", factObject: "Widget", hnAction: "releases", eventAction: "发布", objectQuote: "Widget", hnAnchor: "OpenAI", eventAnchor: "OpenAI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := DiscussionCandidateItem{ID: 23, OriginalTitle: tc.eventTitle, Fact: Fact{Subject: "OpenAI", Action: tc.eventAction, Object: tc.factObject}}
			if tc.occurredAt != "" {
				item.Fact.OccurredAt = &tc.occurredAt
			}
			input := DiscussionMatchInput{Thread: HNThread{ID: 100, Title: tc.hnTitle}, Candidates: []DiscussionCandidate{{StoryID: 17, Items: []DiscussionCandidateItem{item}}}}
			match := DiscussionMatch{Decision: DiscussionMatchSame, StoryID: 17, Evidence: []DiscussionMatchEvidence{
				{Role: "subject", HNField: "title", HNQuote: "OpenAI", ItemID: 23, EventField: "fact.subject", EventQuote: "OpenAI"},
				{Role: "action", HNField: "title", HNQuote: tc.hnAction, ItemID: 23, EventField: "fact.action", EventQuote: tc.eventAction},
				{Role: "object", HNField: "title", HNQuote: tc.objectQuote, ItemID: 23, EventField: "fact.object", EventQuote: tc.objectQuote},
				{Role: "anchor", HNField: "title", HNQuote: tc.hnAnchor, ItemID: 23, EventField: "original_title", EventQuote: tc.eventAnchor},
			}}
			if _, err := ValidateDiscussionMatch(input, match); err == nil {
				t.Fatalf("accepted different occurrence or insufficient anchor: %+v", match)
			}
		})
	}
}

func TestHNMatchingRejectsGenericProductURLAsOccurrenceAnchor(t *testing.T) {
	const productURL = "https://example.com/products/widget"
	input := DiscussionMatchInput{Thread: HNThread{ID: 100, Title: "OpenAI releases Widget", URL: productURL}, Candidates: []DiscussionCandidate{{StoryID: 17, Items: []DiscussionCandidateItem{{ID: 23, OriginalTitle: "OpenAI releases Widget", URL: productURL, Fact: Fact{Subject: "OpenAI", Action: "发布", Object: "Widget"}}}}}}
	match := DiscussionMatch{Decision: DiscussionMatchSame, StoryID: 17, Evidence: []DiscussionMatchEvidence{
		{Role: "subject", HNField: "title", HNQuote: "OpenAI", ItemID: 23, EventField: "fact.subject", EventQuote: "OpenAI"},
		{Role: "action", HNField: "title", HNQuote: "releases", ItemID: 23, EventField: "fact.action", EventQuote: "发布"},
		{Role: "object", HNField: "title", HNQuote: "Widget", ItemID: 23, EventField: "fact.object", EventQuote: "Widget"},
		{Role: "anchor", HNField: "url", HNQuote: productURL, ItemID: 23, EventField: "url", EventQuote: productURL},
	}}
	if _, err := ValidateDiscussionMatch(input, match); err == nil {
		t.Fatal("generic product URL approved as an occurrence anchor")
	}
}
