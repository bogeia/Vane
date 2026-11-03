package biz

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// DiscussionCandidate supplies event facts, never comment assertions, to the matcher.
type DiscussionCandidate struct {
	StoryID int64
	Items   []DiscussionCandidateItem
}

type DiscussionCandidateItem struct {
	ID            int64
	URL           string
	OriginalTitle string
	Summary       string
	PublishedAt   time.Time
	Fact          Fact
}

type DiscussionMatchInput struct {
	Thread     HNThread
	Candidates []DiscussionCandidate
}

type DiscussionMatchDecision string

const (
	DiscussionMatchSame        DiscussionMatchDecision = "same_occurrence"
	DiscussionMatchDifferent   DiscussionMatchDecision = "different"
	DiscussionMatchUnknown     DiscussionMatchDecision = "unknown"
	DiscussionMatchRuleVersion                         = "hn-occurrence-v1"
)

// Every quote must be an exact, nonempty substring of the named original field.
// HNField is title or url; EventField is original_title, summary, url,
// fact.title, fact.subject, fact.action, fact.object, or fact.occurred_at.
// Role is subject, action, object, or anchor. A same-occurrence decision requires
// all four roles; anchor must identify a full version/model, explicit occurrence
// date, or unique announcement/paper/repository, never company alone.
type DiscussionMatchEvidence struct {
	Role       string
	HNField    string
	HNQuote    string
	ItemID     int64
	EventField string
	EventQuote string
}

type DiscussionMatch struct {
	Decision DiscussionMatchDecision
	StoryID  int64
	Evidence []DiscussionMatchEvidence
}

// MatchDiscussion returns one decision for the one supplied thread. All candidate
// events must be considered together; ambiguity must return unknown. Comments are
// deliberately absent. Callers validate citations and reject conflicting anchors.
type DiscussionMatcher interface {
	MatchDiscussion(context.Context, DiscussionMatchInput) (DiscussionMatch, error)
}

// DiscussionCandidateField is shared with protocol adapters so evidence names
// always refer to exactly one original input field.
func DiscussionCandidateField(item DiscussionCandidateItem, field string) string {
	switch field {
	case "url":
		return item.URL
	case "original_title":
		return item.OriginalTitle
	case "summary":
		return item.Summary
	case "fact.title":
		return item.Fact.Title
	case "fact.subject":
		return item.Fact.Subject
	case "fact.action":
		return item.Fact.Action
	case "fact.object":
		return item.Fact.Object
	case "fact.occurred_at":
		if item.Fact.OccurredAt != nil {
			return *item.Fact.OccurredAt
		}
	}
	return ""
}

func DiscussionMatchSignature(input DiscussionMatchInput) string {
	content, _ := json.Marshal(input)
	return fmt.Sprintf("%s:%x", DiscussionMatchRuleVersion, sha256.Sum256(content))
}

// ValidateDiscussionMatch rejects invented citations and explicit occurrence
// conflicts even if a model returns same_occurrence with high confidence.
func ValidateDiscussionMatch(input DiscussionMatchInput, match DiscussionMatch) (DiscussionMatch, error) {
	invalid := func() (DiscussionMatch, error) {
		return DiscussionMatch{}, errors.New("invalid HN occurrence evidence")
	}
	if match.Decision != DiscussionMatchSame {
		if (match.Decision != DiscussionMatchDifferent && match.Decision != DiscussionMatchUnknown) || match.StoryID != 0 || len(match.Evidence) != 0 {
			return invalid()
		}
		return match, nil
	}
	if len(match.Evidence) != 4 {
		return invalid()
	}
	var candidate *DiscussionCandidate
	for i := range input.Candidates {
		if input.Candidates[i].StoryID == match.StoryID {
			candidate = &input.Candidates[i]
			break
		}
	}
	if candidate == nil {
		return invalid()
	}
	for _, other := range input.Candidates {
		if other.StoryID == candidate.StoryID {
			continue
		}
		for _, left := range candidate.Items {
			for _, right := range other.Items {
				if strings.EqualFold(left.Fact.Subject, right.Fact.Subject) && strings.EqualFold(left.Fact.Action, right.Fact.Action) && strings.EqualFold(left.Fact.Object, right.Fact.Object) && discussionSameDate(left.Fact.OccurredAt, right.Fact.OccurredAt) {
					return invalid()
				}
			}
		}
	}
	roles := map[string]bool{}
	for _, e := range match.Evidence {
		if (e.Role != "subject" && e.Role != "action" && e.Role != "object" && e.Role != "anchor") || roles[e.Role] {
			return invalid()
		}
		roles[e.Role] = true
		hn := ""
		switch e.HNField {
		case "title":
			hn = input.Thread.Title
		case "url":
			hn = input.Thread.URL
		default:
			return invalid()
		}
		var item *DiscussionCandidateItem
		for i := range candidate.Items {
			if candidate.Items[i].ID == e.ItemID {
				item = &candidate.Items[i]
				break
			}
		}
		if item == nil || strings.TrimSpace(e.HNQuote) == "" || strings.TrimSpace(e.EventQuote) == "" || len(e.HNQuote) > 2048 || len(e.EventQuote) > 4096 || !strings.Contains(hn, e.HNQuote) || !strings.Contains(DiscussionCandidateField(*item, e.EventField), e.EventQuote) {
			return invalid()
		}
		// Background in a summary cannot override the main original title and fact.
		if discussionOccurrenceConflict(input.Thread, *item) {
			return invalid()
		}
		if e.Role == "action" {
			hnAction, eventAction := discussionAction(e.HNQuote), discussionAction(e.EventQuote)
			if hnAction == "" || eventAction == "" || hnAction != eventAction {
				return invalid()
			}
		}
		if e.Role == "anchor" && !discussionStrongAnchor(e.HNQuote, e.EventQuote) {
			return invalid()
		}
	}
	return match, nil
}

var (
	discussionVersions = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9]*[-_ ]+)?[0-9]+(?:\.[0-9]+)+(?:[-_.][a-z0-9]+)*|[0-9]+(?:\.[0-9]+)?[bBmMkK]\b|[a-z]+[-_][a-z]*[0-9]+(?:[-_.][a-z0-9]+)*|[a-z]+[0-9]+[a-z0-9]*(?:[-_.][a-z0-9]+)*`)
	discussionYears    = regexp.MustCompile(`\b(?:19|20)[0-9]{2}\b`)
	discussionDates    = regexp.MustCompile(`\b(?:19|20)[0-9]{2}[-/][0-9]{2}[-/][0-9]{2}\b`)
	discussionAnchor   = regexp.MustCompile(`(?i)[a-z0-9][a-z0-9._/-]*[0-9][a-z0-9._/-]*`)
)

func discussionOccurrenceConflict(thread HNThread, item DiscussionCandidateItem) bool {
	hn := strings.ToLower(thread.Title)
	event := strings.ToLower(item.OriginalTitle + " " + item.Fact.Title + " " + item.Fact.Object)
	if hnAction, eventAction := discussionAction(hn), discussionAction(item.Fact.Action); hnAction != "" && eventAction != "" && hnAction != eventAction {
		return true
	}
	for _, label := range []string{"beta", "preview", "alpha", "release candidate", "mini", "turbo", "nano", "pro", "max", "ultra", "lite"} {
		if discussionHasWord(hn, label) != discussionHasWord(event, label) {
			return true
		}
	}
	versions := func(text string) []string {
		values := discussionVersions.FindAllString(text, -1)
		for i := range values {
			values[i] = strings.ReplaceAll(values[i], " ", "-")
		}
		return values
	}
	a, b := versions(hn), versions(event)
	if len(a) > 0 || len(b) > 0 {
		for _, v := range a {
			if !slices.Contains(b, v) {
				return true
			}
		}
		for _, v := range b {
			if !slices.Contains(a, v) {
				return true
			}
		}
	}
	if item.Fact.OccurredAt != nil {
		if _, err := time.Parse(time.DateOnly, *item.Fact.OccurredAt); err != nil {
			return true
		}
		for _, year := range discussionYears.FindAllString(hn, -1) {
			if year != (*item.Fact.OccurredAt)[:4] {
				return true
			}
		}
		for _, date := range discussionDates.FindAllString(hn, -1) {
			if strings.ReplaceAll(date, "/", "-") != *item.Fact.OccurredAt {
				return true
			}
		}
	}
	return false
}

func discussionHasWord(text, word string) bool {
	return regexp.MustCompile(`(?i)(^|[^a-z0-9])` + regexp.QuoteMeta(word) + `([^a-z0-9]|$)`).MatchString(text)
}

func discussionStrongAnchor(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	// Full identifiers need letters as well as digits. Bare years, numbers and
	// abbreviated versions do not distinguish a concrete model or announcement.
	for _, token := range discussionAnchor.FindAllString(a, -1) {
		if len(token) < 5 || !strings.ContainsFunc(token, unicode.IsLetter) || !strings.ContainsAny(token, "-_.") {
			continue
		}
		if discussionHasWord(b, token) {
			return true
		}
	}
	for _, date := range discussionDates.FindAllString(a, -1) {
		if strings.Contains(b, date) {
			return true
		}
	}
	// Only a specific announcement slug qualifies; generic collection URLs do not.
	for _, text := range []string{a, b} {
		u, err := url.Parse(text)
		if err != nil || !u.IsAbs() {
			continue
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		slug := parts[len(parts)-1]
		if len(slug) < 12 || !strings.Contains(slug, "-") {
			continue
		}
		if slices.Contains([]string{"product-updates", "latest-releases", "release-notes", "announcements"}, slug) {
			continue
		}
		if strings.Contains(a, u.Host+u.Path) && strings.Contains(b, u.Host+u.Path) {
			return true
		}
	}
	return false
}

func discussionSameDate(a, b *string) bool {
	if a == nil || b == nil {
		return true
	}
	return *a == *b
}

type DiscussionEventItem struct {
	DiscussionCandidateItem
	Selected bool
	Roundup  bool
}

func DiscussionTargetSignature(storyID int64, items []DiscussionEventItem) string {
	content, _ := json.Marshal(struct {
		StoryID int64
		Items   []DiscussionEventItem
	}{storyID, items})
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

// BuildDiscussionCandidate owns bounded evidence selection, preserving complete
// Fact values and all version characters in the original title.
func BuildDiscussionCandidate(storyID int64, items []DiscussionEventItem) DiscussionCandidate {
	items = slices.Clone(items)
	slices.SortFunc(items, func(a, b DiscussionEventItem) int {
		if a.Selected != b.Selected {
			if a.Selected {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.ID, b.ID)
	})
	candidate := DiscussionCandidate{StoryID: storyID}
	for _, item := range items {
		if item.Roundup || item.Fact.Subject == "" || item.Fact.Action == "" || item.Fact.Object == "" {
			continue
		}
		value := item.DiscussionCandidateItem
		value.OriginalTitle = truncate(value.OriginalTitle, 256)
		value.Summary = truncate(value.Summary, 400)
		candidate.Items = append(candidate.Items, value)
		if len(candidate.Items) == DiscussionCandidateItemLimit {
			break
		}
	}
	return candidate
}

// Conservative action families keep a follow-up assessment or price change from
// borrowing the original release's product identifier.
func discussionAction(text string) string {
	text = strings.ToLower(text)
	for _, group := range []struct {
		name  string
		words []string
	}{
		{"assessment", []string{"benchmark", "benchmarks", "review", "reviews", "evaluation", "evaluates", "评测", "测试", "测评"}},
		{"pricing", []string{"price", "prices", "pricing", "降价", "调价", "价格"}},
		{"acquisition", []string{"acquires", "acquisition", "收购"}},
		{"funding", []string{"raises", "funding", "融资"}},
		{"open_source", []string{"open-source", "open-sources", "open-sourced", "open source", "开源"}},
		{"release", []string{"release", "releases", "released", "launch", "launches", "launched", "introduces", "introducing", "announces", "announcement", "发布", "推出", "上线", "开放"}},
	} {
		for _, word := range group.words {
			if strings.ContainsFunc(word, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
				if strings.Contains(text, word) {
					return group.name
				}
			} else if discussionHasWord(text, word) {
				return group.name
			}
		}
	}
	return ""
}
