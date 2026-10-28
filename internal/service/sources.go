package service

import (
	"context"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/service/opml"
)

type SourcesService struct {
	sources *biz.SourcesUseCase
	icons   *biz.SourceIconsUseCase
}

func NewSourcesService(sources *biz.SourcesUseCase, icons *biz.SourceIconsUseCase) *SourcesService {
	return &SourcesService{sources: sources, icons: icons}
}

func (s *SourcesService) OPML(_ context.Context, site config.Site) ([]byte, error) {
	return opml.Marshal(site.Name, s.sources.Enabled())
}
