package integration

import (
	"bytes"
	"testing"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/data"
)

func TestSourceIconsPersistAndReachHistoricalReportAndItems(t *testing.T) {
	store, now := groupStore(t)
	repo := data.NewSourceIconsRepo(store)
	source := biz.Source{ID: "group", FeedURL: "https://example.test/feed"}
	icon := biz.SourceIcon{Content: []byte("original artwork"), ContentType: "image/png"}
	if err := repo.SaveIcon(t.Context(), source, "input", icon); err != nil {
		t.Fatal(err)
	}
	// A separate repository instance reads the persistent cache, independently of the web build directory.
	saved, err := data.NewSourceIconsRepo(store).Icon(t.Context(), source.ID)
	if err != nil || !bytes.Equal(saved.Content, icon.Content) || saved.ContentType != icon.ContentType {
		t.Fatalf("cache: %v", err)
	}
	if err := data.Migrate(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	input, err := repo.IconState(t.Context(), source.ID)
	if err != nil || input.Input != "input" || input.Version != icon.Version() {
		t.Fatalf("migration changed cache: %+v %v", input, err)
	}
	item := groupItem(t, store, now, 1)
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET stage='done',processed_at=$1 WHERE id=$2`, now, item.ID); err != nil {
		t.Fatal(err)
	}
	items, err := data.NewItemsRepo(store).ListItems(t.Context(), nil, 10, biz.ItemFilter{})
	if err != nil || len(items) != 1 || items[0].Source.IconVersion != icon.Version() {
		t.Fatalf("public item icon: %+v %v", items, err)
	}
	var storyID int64
	if err := testDB(t, store).QueryRowContext(t.Context(), `INSERT INTO stories(title) VALUES('Icon story') RETURNING id`).Scan(&storyID); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET story_id=$1 WHERE id=$2`, storyID, item.ID); err != nil {
		t.Fatal(err)
	}
	mentions, err := data.NewHotRepo(store).HotMentions(t.Context(), now, now)
	if err != nil || len(mentions) != 1 || mentions[0].Source.IconVersion != icon.Version() {
		t.Fatalf("hot icon: %+v %v", mentions, err)
	}
	// Previously generated reports have no icon metadata; serving them enriches publisher artwork in one query.
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO reports(date,content) VALUES('2026-10-22',$1::jsonb)`, `{"date":"2026-10-22","headline":{"id":"1","title":"Historical","source":{"id":"group","name":"Group","tier":"official"}},"highlights":[],"sections":[],"briefs":[]}`); err != nil {
		t.Fatal(err)
	}
	report, err := data.NewReportsRepo(store).Report(t.Context(), "2026-10-22")
	if err != nil || report.Headline == nil || report.Headline.Source.IconVersion != icon.Version() {
		t.Fatalf("historical report icon: %+v %v", report, err)
	}
	// Reusing a saved report must return the same live metadata as a direct read.
	cached, err := data.NewReportsRepo(store).GenerateReport(t.Context(), biz.ReportRequest{Date: "2026-10-22"}, func([]biz.ReportCandidate, []biz.Report) (biz.Report, error) {
		t.Fatal("cached report was recompiled")
		return biz.Report{}, nil
	})
	if err != nil || cached.Headline == nil || cached.Headline.Source.IconVersion != icon.Version() || cached.Headline.Title != "Historical" {
		t.Fatalf("cached report icon: %+v %v", cached, err)
	}
	// A late fetch for an old feed address cannot overwrite the current publisher's cache.
	source.FeedURL = "https://example.test/old-feed"
	if err := repo.SaveIcon(t.Context(), source, "wrong", biz.SourceIcon{}); err != nil {
		t.Fatal(err)
	}
	saved, err = repo.Icon(t.Context(), source.ID)
	if err != nil || saved.Version() != icon.Version() {
		t.Fatal("stale fetch replaced current source icon")
	}
}
