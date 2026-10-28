package service

import (
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func TestCursorProtocol(t *testing.T) {
	stamp := time.Date(2026, 10, 23, 8, 0, 0, 123, time.FixedZone("offset", 8*60*60))
	want := "2026-10-23T08:00:00.000000123+08:00|9223372036854775807"
	encoded := encodeCursor(&biz.Cursor{PublishedAt: stamp, ID: 9223372036854775807})
	if encoded != want {
		t.Fatalf("cursor=%q", encoded)
	}
	cursor, err := parseCursor(want)
	if err != nil || !cursor.PublishedAt.Equal(stamp) || cursor.ID != 9223372036854775807 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	if empty, err := parseCursor(""); empty != nil || err != nil || encodeCursor(nil) != "" {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	for _, value := range []string{"bad", "2026-10-23T00:00:00Z", "2026-10-23T00:00:00Z|0", "2026-10-23T00:00:00Z|-1", "2026-10-23T00:00:00Z|9223372036854775808", "2026-10-23T00:00:00Z|1|2", strings.Repeat("x", 129)} {
		if _, err := parseCursor(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}
