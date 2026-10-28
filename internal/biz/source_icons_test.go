package biz

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type iconRepoStub struct {
	mu             sync.Mutex
	input          string
	icon           SourceIcon
	saves          int
	missingVersion bool
}

func (r *iconRepoStub) IconState(context.Context, string) (SourceIconState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	version := r.icon.Version()
	if r.missingVersion {
		version = ""
	}
	return SourceIconState{Input: r.input, Version: version}, nil
}

func (r *iconRepoStub) SaveIcon(_ context.Context, _ Source, input string, icon SourceIcon) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.input, r.icon = input, icon
	r.missingVersion = false
	r.saves++
	return nil
}

func (r *iconRepoStub) Icon(context.Context, string) (SourceIcon, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.icon, nil
}

type iconFetcherStub struct {
	calls int
	fail  bool
	wait  bool
}

func (f *iconFetcherStub) DiscoverIcon(ctx context.Context, _ Source) (SourceIcon, error) {
	f.calls++
	if f.wait {
		<-ctx.Done()
		return SourceIcon{}, ctx.Err()
	}
	if f.fail {
		return SourceIcon{}, errors.New("discovery failed")
	}
	return SourceIcon{Content: []byte("original"), ContentType: "image/png"}, nil
}

func TestSourceIconsCacheAndInvalidatesChangedInputs(t *testing.T) {
	repo := &iconRepoStub{}
	fetcher := &iconFetcherStub{}
	sources := Sources{{ID: "publisher", Kind: SourceKindRSS, FeedURL: "https://publisher.test/feed", Enabled: true}}
	u := NewSourceIconsUseCase(sources, repo, fetcher)
	if err := u.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	if repo.icon.Version() == "" || string(repo.icon.Content) != "original" {
		t.Fatal("missing versioned original")
	}
	if err := u.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fetcher.calls != 1 {
		t.Fatal("cached icon downloaded again")
	}
	for _, change := range []func(){func() { sources[0].IconURL = "https://publisher.test/manual.png" }, func() { sources[0].FeedURL = "https://publisher.test/new-feed" }} {
		change()
		before := fetcher.calls
		if err := u.Complete(t.Context()); err != nil {
			t.Fatal(err)
		}
		if fetcher.calls != before+1 {
			t.Fatal("changed input did not invalidate cache")
		}
	}

	// Missing resource metadata preserves usable artwork on transient discovery failure.
	repo.missingVersion = true
	fetcher.fail = true
	before := repo.icon.Version()
	if err := u.Complete(t.Context()); err == nil || repo.icon.Version() != before {
		t.Fatal("resource completion lost existing icon")
	}
	fetcher.fail = false
	if err := u.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	sources[0].IconURL = "https://publisher.test/failed.png"
	fetcher.fail = true
	if err := u.Complete(t.Context()); err == nil {
		t.Fatal("missing discovery error")
	}
	if repo.icon.Version() != "" || repo.input != "" {
		t.Fatal("failed override kept previous publisher artwork")
	}
	fetcher.fail = false
	if err := u.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSourceIconCompletionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	u := NewSourceIconsUseCase(Sources{{ID: "a", Enabled: true}}, &iconRepoStub{}, &iconFetcherStub{wait: true})
	if err := u.Complete(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestSourceIconVersion(t *testing.T) {
	if got := (SourceIcon{}).Version(); got != "" {
		t.Fatalf("missing artwork version=%q", got)
	}
	icon := SourceIcon{Content: []byte("abc"), ContentType: "image/png"}
	if got := icon.Version(); got != "ba7816bf8f01cfea414140de" {
		t.Fatalf("version=%q", got)
	}
	icon.ContentType = "image/svg+xml"
	if got := icon.Version(); got != "ba7816bf8f01cfea414140de" {
		t.Fatalf("content version depends on transport metadata: %q", got)
	}
}

func TestSourceIconsIncompleteResourceRetriesWithoutDiscardingArtwork(t *testing.T) {
	repo := &iconRepoStub{}
	fetcher := &iconFetcherStub{}
	u := NewSourceIconsUseCase(Sources{{ID: "publisher", Kind: SourceKindRSS, FeedURL: "https://publisher.test/feed", Enabled: true}}, repo, fetcher)
	if err := u.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo.missingVersion = true
	fetcher.fail = true
	before, saves := string(repo.icon.Content), repo.saves
	if err := u.Complete(t.Context()); err == nil {
		t.Fatal("missing discovery failure")
	}
	if string(repo.icon.Content) != before || repo.saves != saves {
		t.Fatal("incomplete resource discarded usable content")
	}
	fetcher.fail = false
	if err := u.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	if repo.missingVersion || repo.saves != saves+1 {
		t.Fatal("resource not completed")
	}
	calls := fetcher.calls
	if err := u.Complete(t.Context()); err != nil || fetcher.calls != calls {
		t.Fatal("complete resource not reused")
	}
}
