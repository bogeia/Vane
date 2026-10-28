package service

import (
	"context"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
)

type MetaOutput struct {
	Body struct {
		Name       string   `json:"name"`
		Categories []string `json:"categories"`
	}
}

func Meta(site config.Site) func(context.Context, *struct{}) (*MetaOutput, error) {
	return func(context.Context, *struct{}) (*MetaOutput, error) {
		output := &MetaOutput{}
		output.Body.Name = site.Name
		output.Body.Categories = biz.Categories
		return output, nil
	}
}
