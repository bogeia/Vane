package data

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func assertSnapshotJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var actual, expected any
	if err := json.Unmarshal(got, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("snapshot=%s\nwant=%s", got, want)
	}
}

func TestReportSnapshotPersistenceContract(t *testing.T) {
	const document = `{"date":"2026-10-23","headline":{"id":"9007199254740993","title":"中文标题","originalTitle":"Original","summary":"摘要","reason":"理由","url":"https://example.test/a","imageUrl":"","source":{"id":"source","name":"Source","tier":"official"},"publishedAt":"2026-10-22T12:00:00Z","score":87,"selected":true,"category":"论文","tags":[],"storyId":"12","storyOthers":2,"storyItemIds":["1","2"]},"highlights":[],"sections":[{"category":"论文","items":[]}],"briefs":null}`
	report, err := decodeReport([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if report.Headline == nil || report.Headline.ID != "9007199254740993" || report.Headline.Score != 87 || report.Headline.Source.Tier != "official" || report.Highlights == nil || report.Briefs != nil || len(report.Sections) != 1 || report.Sections[0].Items == nil {
		t.Fatalf("decoded=%+v", report)
	}
	now, raw := time.Now(), 99
	report.PushedAt = &now
	report.Headline.Source.IconVersion = "/api/source-icons/source?v=live"
	report.Headline.Roundup = true
	report.Headline.Score1, report.Headline.Score2 = &raw, &raw
	encoded, err := encodeReport(report)
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshotJSON(t, encoded, document)
	for _, tc := range []struct {
		report biz.Report
		want   string
	}{
		{biz.Report{}, `{"date":"","headline":null,"highlights":null,"sections":null,"briefs":null}`},
		{biz.Report{Highlights: []biz.Item{}, Sections: []biz.ReportSection{}, Briefs: []biz.Item{}}, `{"date":"","headline":null,"highlights":[],"sections":[],"briefs":[]}`},
	} {
		encoded, err := encodeReport(tc.report)
		if err != nil {
			t.Fatal(err)
		}
		assertSnapshotJSON(t, encoded, tc.want)
	}
	if _, err := decodeReport([]byte(`{"headline":`)); err == nil {
		t.Fatal("accepted invalid snapshot")
	}
}

func TestFactSnapshotPreservesNullAndDate(t *testing.T) {
	for _, document := range []string{`null`, `{"title":"发布","subject":"OpenAI","action":"发布","object":"模型","occurredAt":null}`, `{"title":"发布","subject":"OpenAI","action":"发布","object":"模型","occurredAt":"2026-10-22"}`} {
		fact, err := decodeFact([]byte(document))
		if err != nil {
			t.Fatal(err)
		}
		if document == "null" && fact != nil {
			t.Fatal("null fact became a value")
		}
		if fact != nil && (fact.Title != "发布" || fact.Subject != "OpenAI" || fact.Action != "发布" || fact.Object != "模型") {
			t.Fatalf("fact=%+v", fact)
		}
		encoded, err := encodeFact(fact)
		if err != nil {
			t.Fatal(err)
		}
		assertSnapshotJSON(t, encoded, document)
	}
	if _, err := decodeFact([]byte(`{"occurredAt":12}`)); err == nil {
		t.Fatal("accepted invalid fact date")
	}
}
