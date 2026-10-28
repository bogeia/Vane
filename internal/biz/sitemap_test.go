package biz

import (
	"context"
	"errors"
	"testing"
	"time"
)

type sitemapRepository struct {
	ItemsRepository
	reports, items                      []SitemapResource
	reportErr, itemErr                  error
	reportLimit, reportCalls, itemCalls int
	query                               SitemapQuery
}

func (r *sitemapRepository) SitemapReports(_ context.Context, limit int) ([]SitemapResource, error) {
	r.reportCalls++
	r.reportLimit = limit
	return r.reports, r.reportErr
}

func (r *sitemapRepository) SitemapItems(_ context.Context, query SitemapQuery) ([]SitemapResource, error) {
	r.itemCalls++
	r.query = query
	return r.items, r.itemErr
}

func TestSitemapUseCaseOwnsPriorityWindowAndLimit(t *testing.T) {
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	report := SitemapResource{Kind: SitemapReport, ID: "2026-10-23", LastModified: now}
	first := SitemapResource{Kind: SitemapItem, ID: "3", LastModified: now}
	second := SitemapResource{Kind: SitemapItem, ID: "2", LastModified: now.Add(-time.Hour)}
	for _, tc := range []struct {
		name      string
		limit     int
		want      []SitemapResource
		itemCalls int
	}{
		{"zero", 0, []SitemapResource{}, 0},
		{"report fills cap", 1, []SitemapResource{report}, 0},
		{"items fill remainder", 2, []SitemapResource{report, first}, 1},
		{"both item rows", 3, []SitemapResource{report, first, second}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &sitemapRepository{reports: []SitemapResource{report}, items: []SitemapResource{first, second}}
			got, err := NewItemsUseCase(repo, time.Now).SitemapPages(t.Context(), now, tc.limit)
			if err != nil || got == nil || len(got) != len(tc.want) {
				t.Fatalf("pages=%+v err=%v", got, err)
			}
			for index := range got {
				if got[index] != tc.want[index] {
					t.Fatalf("page=%+v want=%+v", got[index], tc.want[index])
				}
			}
			if repo.itemCalls != tc.itemCalls {
				t.Fatalf("item queries=%d", repo.itemCalls)
			}
			if tc.limit == 0 {
				if repo.reportCalls != 0 {
					t.Fatal("zero limit queried repository")
				}
				return
			}
			if repo.reportLimit != tc.limit {
				t.Fatalf("report limit=%d", repo.reportLimit)
			}
			if tc.itemCalls > 0 && (repo.query.Limit != tc.limit-1 || !repo.query.Since.Equal(now.Add(-365*24*time.Hour)) || !repo.query.Until.Equal(now)) {
				t.Fatalf("query=%+v", repo.query)
			}
		})
	}
}

func TestSitemapUseCasePropagatesQueryFailure(t *testing.T) {
	failure := errors.New("query failure")
	for _, repo := range []*sitemapRepository{{reportErr: failure}, {itemErr: failure}} {
		if _, err := NewItemsUseCase(repo, time.Now).SitemapPages(t.Context(), time.Now(), 2); !errors.Is(err, failure) {
			t.Fatalf("error=%v", err)
		}
	}
}
