package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrSourceIconNotFound = errors.New("source icon not found")

type SourceIcon struct {
	Content     []byte
	ContentType string
}

// Version identifies the exact stored artwork; missing artwork has no resource version.
func (icon SourceIcon) Version() string {
	if len(icon.Content) == 0 {
		return ""
	}
	digest := sha256.Sum256(icon.Content)
	return fmt.Sprintf("%x", digest[:12])
}

type SourceIconState struct{ Input, Version string }

type SourceIconsRepository interface {
	IconState(context.Context, string) (SourceIconState, error)
	SaveIcon(context.Context, Source, string, SourceIcon) error
	Icon(context.Context, string) (SourceIcon, error)
}
type SourceIconFetcher interface {
	DiscoverIcon(context.Context, Source) (SourceIcon, error)
}
type SourceIconsUseCase struct {
	sources Sources
	repo    SourceIconsRepository
	fetcher SourceIconFetcher
}

func NewSourceIconsUseCase(sources Sources, repo SourceIconsRepository, fetcher SourceIconFetcher) *SourceIconsUseCase {
	return &SourceIconsUseCase{sources: sources, repo: repo, fetcher: fetcher}
}

func (u *SourceIconsUseCase) Icon(ctx context.Context, id string) (SourceIcon, error) {
	return u.repo.Icon(ctx, id)
}

// Complete fills missing icons and refreshes changed inputs; successful icons are reused.
func (u *SourceIconsUseCase) Complete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	jobs := make(chan Source)
	var workers sync.WaitGroup
	var mu sync.Mutex
	var failures error
	for range 4 {
		workers.Go(func() {
			for source := range jobs {
				input := fmt.Sprintf("%x", sha256.Sum256([]byte(source.Kind+"\x00"+source.FeedURL+"\x00"+source.IconURL)))
				input = "smallest-64-v1:" + input
				saved, err := u.repo.IconState(ctx, source.ID)
				if err == nil && saved.Input == input && saved.Version != "" {
					continue
				}
				if err == nil && saved.Input != input {
					err = u.repo.SaveIcon(ctx, source, "", SourceIcon{})
				}
				if err == nil {
					attempt, cancel := context.WithTimeout(ctx, 45*time.Second)
					var icon SourceIcon
					icon, err = u.fetcher.DiscoverIcon(attempt, source)
					cancel()
					if err == nil {
						err = u.repo.SaveIcon(ctx, source, input, icon)
					}
				}
				if err != nil {
					mu.Lock()
					failures = errors.Join(failures, fmt.Errorf("source %s icon: %w", source.ID, err))
					mu.Unlock()
				}
			}
		})
	}
	for _, source := range u.sources {
		if !source.Enabled {
			continue
		}
		select {
		case jobs <- source:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	workers.Wait()
	return errors.Join(failures, ctx.Err())
}
