package biz

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

const (
	SourceKindRSS      = "rss"
	SourceKindWebList  = "web_list"
	SourceKindHFModels = "hf_models"
)

type Source struct {
	ID, Name, Tier, FeedURL, Kind string
	IconURL                       string
	Enabled                       bool
	IntervalMinutes               int
}
type (
	Sources         []Source
	SourceCandidate struct{ Name, URL string }
	SourceSkip      struct{ URL, Reason string }
	SourceImport    struct {
		Sources []Source
		Skipped []SourceSkip
	}
)

const (
	SkipSourceDuplicate   = "重复"
	SkipSourceInvalidURL  = "地址非法"
	SkipSourceUnavailable = "打不开"
	SkipSourceParseFailed = "解析失败"
	SkipSourceEmpty       = "没有条目"
	SkipSourceStale       = "太久没更新"
)

var (
	ErrFeedParse      = errors.New("feed parse failed")
	ErrSourceNotFound = errors.New("source not found")
)

type SourcesUseCase struct {
	sources Sources
	fetcher FeedFetcher
	now     func() time.Time
}

func NewSourcesUseCase(sources Sources, fetcher FeedFetcher, now func() time.Time) *SourcesUseCase {
	return &SourcesUseCase{sources: sources, fetcher: fetcher, now: now}
}

func (u *SourcesUseCase) Enabled() []Source {
	result := make([]Source, 0)
	for _, source := range u.sources {
		if source.Enabled {
			result = append(result, source)
		}
	}
	slices.SortFunc(result, func(a, b Source) int { return strings.Compare(a.ID, b.ID) })
	return result
}

func NormalizeSourceURL(raw string) (string, error) {
	normalized, err := normalizeURL(raw)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port != "" && !((parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443")) {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	parsed.Host, parsed.Scheme = host, ""
	parsed.RawPath = strings.TrimRight(parsed.EscapedPath(), "/")
	parsed.Path, _ = url.PathUnescape(parsed.RawPath)
	return strings.TrimPrefix(parsed.String(), "//"), nil
}

var sourceSlug = regexp.MustCompile(`[^a-z0-9]+`)

func SourceID(raw string, used map[string]bool) string {
	parsed, _ := url.Parse(raw)
	base := "rss-" + strings.Trim(sourceSlug.ReplaceAllString(strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")+strings.ToLower(parsed.Path), "-"), "-")
	id := base
	for suffix := 2; used[id]; suffix++ {
		id = fmt.Sprintf("%s-%d", base, suffix)
	}
	used[id] = true
	return id
}

func (u *SourcesUseCase) Import(ctx context.Context, candidates []SourceCandidate, tier string, interval int) (SourceImport, error) {
	result := SourceImport{}
	if !slices.Contains([]string{taxonomy.TierOfficial, taxonomy.TierMedia}, tier) || interval <= 0 || int64(interval) > int64((1<<63-1)/time.Minute) {
		return result, fmt.Errorf("invalid tier or interval")
	}
	seen, outputSeen, ids := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, source := range u.sources {
		normalized, _ := NormalizeSourceURL(source.FeedURL)
		seen[normalized] = true
		outputSeen[normalized] = true
		ids[source.ID] = true
	}
	type probe struct {
		candidate SourceCandidate
		finalURL  string
		reason    string
	}
	probes := make([]probe, 0)
	for _, candidate := range candidates {
		normalized, err := NormalizeSourceURL(candidate.URL)
		reason := ""
		if err != nil {
			reason = SkipSourceInvalidURL
		} else if seen[normalized] {
			reason = SkipSourceDuplicate
		}
		if reason != "" {
			result.Skipped = append(result.Skipped, SourceSkip{candidate.URL, reason})
			continue
		}
		seen[normalized] = true
		probes = append(probes, probe{candidate: candidate})
	}
	now := u.now()
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for index := range jobs {
				p := &probes[index]
				feed, err := u.fetcher.Fetch(ctx, CollectSource{FeedURL: p.candidate.URL, Kind: SourceKindRSS})
				switch {
				case errors.Is(err, ErrFeedParse):
					p.reason = SkipSourceParseFailed
				case err != nil:
					p.reason = SkipSourceUnavailable
				case len(feed.Items) == 0:
					p.reason = SkipSourceEmpty
				default:
					latest := time.Time{}
					for _, item := range feed.Items {
						published := publicationTime(item, now)
						if published.After(latest) {
							latest = published
						}
					}
					if latest.Before(now.Add(-30 * 24 * time.Hour)) {
						p.reason = SkipSourceStale
					}
				}
				p.finalURL = feed.URL
			}
		})
	}
	for index := range probes {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, p := range probes {
		if p.reason != "" {
			result.Skipped = append(result.Skipped, SourceSkip{p.candidate.URL, p.reason})
			continue
		}
		finalURL := p.finalURL
		if finalURL == "" {
			finalURL = p.candidate.URL
		}
		normalized, err := NormalizeSourceURL(finalURL)
		if err != nil {
			result.Skipped = append(result.Skipped, SourceSkip{p.candidate.URL, SkipSourceInvalidURL})
			continue
		}
		if outputSeen[normalized] {
			result.Skipped = append(result.Skipped, SourceSkip{p.candidate.URL, SkipSourceDuplicate})
			continue
		}
		outputSeen[normalized] = true
		result.Sources = append(result.Sources, Source{Kind: SourceKindRSS, ID: SourceID(finalURL, ids), Name: p.candidate.Name, Tier: tier, FeedURL: finalURL, Enabled: true, IntervalMinutes: interval})
	}
	return result, nil
}

type SourcePreviewItem struct {
	Title, URL  string
	PublishedAt time.Time
}
type SourcePreview struct {
	ID, Kind string
	Items    []SourcePreviewItem
}

func (u *SourcesUseCase) Preview(ctx context.Context, id string) (SourcePreview, error) {
	for _, source := range u.sources {
		if source.ID != id {
			continue
		}
		feed, err := u.fetcher.Fetch(ctx, CollectSource{ID: source.ID, Kind: source.Kind, FeedURL: source.FeedURL})
		if err != nil {
			return SourcePreview{}, err
		}
		result := SourcePreview{ID: source.ID, Kind: source.Kind, Items: make([]SourcePreviewItem, 0, len(feed.Items))}
		for _, item := range feed.Items {
			var published time.Time
			if item.Published != nil {
				published = *item.Published
			} else if item.Updated != nil {
				published = *item.Updated
			}
			result.Items = append(result.Items, SourcePreviewItem{Title: item.Title, URL: item.URL, PublishedAt: published})
		}
		slices.SortStableFunc(result.Items, func(a, b SourcePreviewItem) int { return b.PublishedAt.Compare(a.PublishedAt) })
		if len(result.Items) > 20 {
			result.Items = result.Items[:20]
		}
		return result, nil
	}
	return SourcePreview{}, ErrSourceNotFound
}
