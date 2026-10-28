package server

import (
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/service"
)

func TestSourcesOPMLGetAndHead(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("SPA fallback"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Site: config.Site{Name: "Vane & AI"}, Web: config.Web{Dir: dir}, Sources: []config.Source{
		{Kind: biz.SourceKindRSS, ID: "z", Name: "Last & 🤖", Tier: "official", FeedURL: "https://z.test/feed?one=1&two=2", Enabled: true},
		{Kind: biz.SourceKindRSS, ID: "a", Name: "First", Tier: "official", FeedURL: "https://a.test/feed", Enabled: true},
		{Kind: biz.SourceKindRSS, ID: "off", Name: "Disabled", Tier: "media", FeedURL: "https://off.test", Enabled: false},
		{Kind: biz.SourceKindRSS, ID: "account", Name: "Account", Tier: "media", FeedURL: "https://account.test", Enabled: true},
		{Kind: biz.SourceKindRSS, ID: "media", Name: "Media", Tier: "media", FeedURL: "https://media.test", Enabled: true},
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sources := service.NewSourcesService(biz.NewSourcesUseCase(biz.NewSources(cfg), nil, time.Now), nil)
	agent, mcpServer := testAgentServices(logger, cfg.Site, fakeItems{})
	router := NewRouter(logger, nil, cfg, nil, nil, nil, nil, sources, agent, mcpServer)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), method, "/sources.opml", nil))
		if response.Code != 200 || response.Header().Get("Cache-Control") != "public, max-age=60" || response.Header().Get("Content-Type") != "text/x-opml; charset=utf-8" {
			t.Fatalf("%s: %d %v", method, response.Code, response.Header())
		}
		if response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal("missing security headers")
		}
		if method == http.MethodHead {
			if response.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
			continue
		}
		var document struct {
			XMLName xml.Name `xml:"opml"`
			Version string   `xml:"version,attr"`
			Title   string   `xml:"head>title"`
			Groups  []struct {
				Text    string `xml:"text,attr"`
				Sources []struct {
					Text  string `xml:"text,attr"`
					Title string `xml:"title,attr"`
					Type  string `xml:"type,attr"`
					URL   string `xml:"xmlUrl,attr"`
				} `xml:"outline"`
			} `xml:"body>outline"`
		}
		if err := xml.Unmarshal(response.Body.Bytes(), &document); err != nil {
			t.Fatal(err)
		}
		if document.Version != "2.0" || document.Title != "Vane & AI 信源" || len(document.Groups) != 2 {
			t.Fatalf("%+v", document)
		}
		for index, title := range []string{"官方一手", "媒体与个人"} {
			if document.Groups[index].Text != title {
				t.Fatal(document.Groups)
			}
		}
		first := document.Groups[0].Sources
		if len(first) != 2 || first[0].Title != "First" || first[1].Title != "Last & 🤖" || first[1].URL != "https://z.test/feed?one=1&two=2" {
			t.Fatal(first)
		}
		if len(document.Groups[1].Sources) != 2 {
			t.Fatal(document.Groups)
		}
		for _, group := range document.Groups {
			for _, source := range group.Sources {
				if source.Type != "rss" || source.Text != source.Title {
					t.Fatal(source)
				}
			}
		}
	}
}
