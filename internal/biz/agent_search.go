package biz

import "context"

type SearchQuery struct {
	Q      string
	Window Window
	Limit  int
}

type SearchResult struct {
	ItemPage
	Expanded bool
}

func (i *ItemsUseCase) Search(ctx context.Context, query SearchQuery) (SearchResult, error) {
	filter := ItemFilter{View: ViewSelected, Q: query.Q, Since: query.Window.Since(i.now())}
	page, err := i.List(ctx, nil, query.Limit, filter)
	if err != nil {
		return SearchResult{}, err
	}
	if len(page.Items) > 0 {
		return SearchResult{ItemPage: page}, nil
	}
	filter.View = ViewAll
	page, err = i.List(ctx, nil, query.Limit, filter)
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{ItemPage: page, Expanded: len(page.Items) > 0}, nil
}
