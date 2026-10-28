package service

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func parseCursor(value string) (*biz.Cursor, error) {
	if value == "" {
		return nil, nil
	}
	stamp, id, ok := strings.Cut(value, "|")
	published, err := time.Parse(time.RFC3339Nano, stamp)
	number, idErr := strconv.ParseInt(id, 10, 64)
	if !ok || len(value) > 128 || err != nil || idErr != nil || number <= 0 {
		return nil, errors.New("invalid cursor")
	}
	return &biz.Cursor{PublishedAt: published, ID: number}, nil
}

func encodeCursor(cursor *biz.Cursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.PublishedAt.Format(time.RFC3339Nano) + "|" + strconv.FormatInt(cursor.ID, 10)
}
