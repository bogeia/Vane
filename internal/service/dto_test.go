package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func TestReportDTOHidesDomainMetadataAndPreservesJSONShape(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	item := biz.Item{ID: "9007199254740993", Roundup: true, Title: "中文", Tags: []string{}, StoryItemIDs: []string{}, Source: biz.ItemSource{ID: "source", Tier: "official"}, PublishedAt: now}
	body, err := json.Marshal(reportDTO(biz.Report{Date: "2026-10-23", PushedAt: &now, Headline: &item, Highlights: []biz.Item{}, Sections: []biz.ReportSection{}, Briefs: []biz.Item{item}}))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	if len(document) != 5 || string(document["highlights"]) != "[]" || string(document["sections"]) != "[]" {
		t.Fatalf("document=%s", body)
	}
	var headline map[string]json.RawMessage
	if err := json.Unmarshal(document["headline"], &headline); err != nil {
		t.Fatal(err)
	}
	if len(headline) != 16 || string(headline["id"]) != `"9007199254740993"` || string(headline["tags"]) != "[]" || string(headline["published_at"]) != `"2026-10-23T00:00:00Z"` {
		t.Fatalf("headline=%s", document["headline"])
	}
	for _, field := range []string{"original_title", "image_url", "published_at", "story_id", "story_others", "story_item_ids"} {
		if _, ok := headline[field]; !ok {
			t.Fatalf("missing public field %q: %s", field, document["headline"])
		}
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(headline["source"], &source); err != nil {
		t.Fatal(err)
	}
	if string(source["icon_url"]) != `""` {
		t.Fatalf("missing icon_url: %s", headline["source"])
	}
	for _, hidden := range []string{"PushedAt", "pushedAt", "roundup", "Score1", "score1"} {
		if strings.Contains(string(body), `"`+hidden+`"`) {
			t.Fatalf("domain metadata leaked: %s", body)
		}
	}
	if itemsDTO(nil) != nil {
		t.Fatal("nil collection must remain null")
	}
	empty, err := json.Marshal(reportDTO(biz.Report{}))
	if err != nil || string(empty) != `{"date":"","headline":null,"highlights":null,"sections":null,"briefs":null}` {
		t.Fatalf("empty=%s err=%v", empty, err)
	}
}

func TestIconVersionReachesItemAndReportDTO(t *testing.T) {
	item := biz.Item{Source: biz.ItemSource{ID: "publisher/name", IconVersion: "ba7816bf8f01cfea414140de"}}
	want := "/api/source-icons/publisher%2Fname?v=ba7816bf8f01cfea414140de"
	if got := itemDTO(item).Source.IconURL; got != want {
		t.Fatalf("item icon=%q", got)
	}
	report := reportDTO(biz.Report{Headline: &item, Highlights: []biz.Item{item}, Sections: []biz.ReportSection{{Items: []biz.Item{item}}}, Briefs: []biz.Item{item}})
	for _, got := range []string{report.Headline.Source.IconURL, report.Highlights[0].Source.IconURL, report.Sections[0].Items[0].Source.IconURL, report.Briefs[0].Source.IconURL} {
		if got != want {
			t.Fatalf("report icon=%q", got)
		}
	}
}

type iconHotRepository struct{ mentions []biz.HotMention }

func (r iconHotRepository) HotMentions(context.Context, time.Time, time.Time) ([]biz.HotMention, error) {
	return r.mentions, nil
}

func TestHotServiceMapsIconVersion(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	repo := iconHotRepository{mentions: []biz.HotMention{
		{StoryID: "1", Source: biz.ItemSource{ID: "a", IconVersion: "ba7816bf8f01cfea414140de"}, PublishedAt: now},
		{StoryID: "1", Source: biz.ItemSource{ID: "b"}, PublishedAt: now},
	}}
	service := NewHotService(biz.NewHotUseCase(repo, func() time.Time { return now }), nil)
	result, err := service.List(t.Context(), nil)
	if err != nil || len(result.Body.Items) != 1 {
		t.Fatalf("hot: %+v %v", result, err)
	}
	if got := result.Body.Items[0].Sources[0].IconURL; got != "/api/source-icons/a?v=ba7816bf8f01cfea414140de" {
		t.Fatalf("hot icon=%q", got)
	}
}
