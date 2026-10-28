package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/fetch"
	"github.com/bogeia/vane/internal/service/opml"
)

func TestSourcesImportProbesFeedsAndReportsSkips(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/broken":
			w.WriteHeader(404)
		case "/invalid":
			fmt.Fprint(w, "this is not a feed")
		case "/empty":
			fmt.Fprint(w, `<rss version="2.0"><channel><title>empty</title></channel></rss>`)
		case "/redirect", "/alias":
			http.Redirect(w, r, "/final", http.StatusFound)
		default:
			published := now
			if r.URL.Path == "/old" {
				published = now.Add(-31 * 24 * time.Hour)
			}
			fmt.Fprintf(w, `<rss version="2.0"><channel><title>AI</title><item><title>News</title><link>https://example.com/news</link><pubDate>%s</pubDate></item></channel></rss>`, published.Format(time.RFC1123Z))
		}
	}))
	defer server.Close()
	parsed, err := opml.Parse(strings.NewReader(fmt.Sprintf(`<opml version="1.0"><body><outline text="AI"><outline title="Fresh" xmlUrl="%s/redirect"/><outline text="Duplicate" xmlUrl="%s/redirect/?utm_source=x"/><outline text="Existing" xmlUrl="%s/existing"/><outline xmlUrl="%s/broken"/><outline xmlUrl="%s/invalid"/><outline xmlUrl="%s/empty"/><outline xmlUrl="%s/old"/><outline xmlUrl="file:///etc/passwd"/></outline></body></opml>`, server.URL, server.URL, server.URL, server.URL, server.URL, server.URL, server.URL)))
	if err != nil {
		t.Fatal(err)
	}
	parsed = append(parsed, biz.SourceCandidate{Name: "Alias", URL: server.URL + "/alias"})
	cfg := config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}}
	// The production adapter and parser are used; only loopback access is enabled for this server.
	fetcher := data.NewFeedFetcher(fetch.New(true, cfg), nil)
	id := biz.SourceID(server.URL+"/final", map[string]bool{})
	importer := biz.NewSourcesUseCase(biz.Sources{{Kind: biz.SourceKindRSS, ID: id, FeedURL: server.URL + "/existing"}}, fetcher, func() time.Time { return now })
	var output, diagnostics bytes.Buffer
	if err := importSources(t.Context(), importer, parsed, "media", 45, &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "# 粘贴到 sources.media 下\n    - ") || strings.Contains(output.String(), "tier:") {
		t.Fatalf("unexpected grouped import output: %s", output.String())
	}
	if !strings.Contains(output.String(), "block: []") {
		t.Fatal("import output missing block")
	}
	var sources []sourceOutput
	if err := yaml.Unmarshal(output.Bytes(), &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources=%+v skips=%s", sources, diagnostics.String())
	}
	sample, err := os.ReadFile("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), sample, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sources.yaml"), []byte("sources:\n  official: []\n  media:\n"+string(output.Bytes())), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("import output cannot load: %v", err)
	}
	source := sources[0]
	if source.ID != id+"-2" || source.FeedURL != server.URL+"/final" || source.Name != "Fresh" || !source.Enabled || source.IntervalMinutes != 45 {
		t.Fatal(source)
	}
	for _, reason := range []string{"重复", "地址非法", "打不开", "解析失败", "没有条目", "太久没更新"} {
		if !strings.Contains(diagnostics.String(), reason) {
			t.Fatalf("missing %s: %s", reason, diagnostics.String())
		}
	}
	if strings.Count(diagnostics.String(), "重复") != 3 {
		t.Fatal(diagnostics.String())
	}
}

func TestSourcesImportCommandExitAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"config", "sources"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "configs", name+".example.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(dir, "vane")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	if body, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, body)
	}
	conf, opml := filepath.Join(dir, "config.yaml"), filepath.Join(dir, "input.opml")
	before, err := os.ReadFile(filepath.Join(dir, "sources.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opml, []byte(`<opml>`), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{"sources", "import", "--conf", conf, "--opml", opml, "--tier", "official", "--interval", "30"}
	for _, tier := range []string{"T1", "T1_5", "T2"} {
		args := append([]string{}, base...)
		args[7] = tier
		output, err := exec.CommandContext(t.Context(), binary, args...).CombinedOutput()
		if err == nil || !strings.Contains(string(output), "invalid tier or interval") {
			t.Fatalf("old tier %s: %v %s", tier, err, output)
		}
	}
	for _, args := range [][]string{{"sources", "import"}, base, append(append([]string{}, base[:7]...), "bad", "--interval", "30"), append(append([]string{}, base[:9]...), "0")} {
		if output, err := exec.CommandContext(t.Context(), binary, args...).CombinedOutput(); err == nil {
			t.Fatalf("accepted %v: %s", args, output)
		}
	}
	if err := os.WriteFile(opml, []byte(`<opml version="2.0"><body><outline xmlUrl="file:///etc/passwd"/></body></opml>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(t.Context(), binary, base...).CombinedOutput(); err != nil || !strings.Contains(string(output), "地址非法") {
		t.Fatalf("skips must exit zero: %v %s", err, output)
	}
	after, err := os.ReadFile(filepath.Join(dir, "sources.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("sources file changed")
	}
}

func TestSourcesImportCommandOutputsLoadableBlock(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"config", "sources"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "configs", name+".example.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<rss version="2.0"><channel><title>Fixture</title><item><title>Fresh</title><link>https://example.test/news</link><pubDate>%s</pubDate></item></channel></rss>`, time.Now().UTC().Format(time.RFC1123Z))
	}))
	defer server.Close()
	// Overlay only the importer assembly to use the existing loopback test seam.
	wirePath, err := filepath.Abs("wire_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(wirePath)
	if err != nil {
		t.Fatal(err)
	}
	original := "func NewSourcesImport(cfg config.Config, now func() time.Time) *biz.SourcesUseCase {\n\tsources := biz.NewSources(cfg)\n\tfeedFetcher := data.NewProductionFeedFetcher(cfg)"
	if strings.Count(string(wire), original) != 1 {
		t.Fatal("importer assembly changed")
	}
	patched := strings.Replace(string(wire), original, strings.Replace(original, "data.NewProductionFeedFetcher(cfg)", "data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil)", 1), 1)
	importLine := `"github.com/bogeia/vane/internal/data"`
	if strings.Count(patched, importLine) != 1 {
		t.Fatal("data import changed")
	}
	patched = strings.Replace(patched, importLine, importLine+"\n\t\"github.com/bogeia/vane/internal/infra/fetch\"", 1)
	wireFixture := filepath.Join(dir, "wire_gen.go")
	if err := os.WriteFile(wireFixture, []byte(patched), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{wirePath: wireFixture}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "vane")
	if body, err := exec.CommandContext(t.Context(), "go", "build", "-overlay", overlayPath, "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, body)
	}
	opml := filepath.Join(dir, "input.opml")
	if err := os.WriteFile(opml, []byte(fmt.Sprintf(`<opml version="2.0"><body><outline text="Fixture" xmlUrl="%s/feed"/></body></opml>`, server.URL)), 0o600); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "config.yaml")
	command := exec.CommandContext(t.Context(), binary, "sources", "import", "--conf", conf, "--opml", opml, "--tier", "official", "--interval", "30")
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	if err != nil {
		t.Fatalf("import: %v %s", err, diagnostics.String())
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("unexpected skips: %s", diagnostics.String())
	}
	var sources []map[string]any
	if err := yaml.Unmarshal(output, &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources=%s", output)
	}
	block, ok := sources[0]["block"].([]any)
	if !ok || len(block) != 0 {
		t.Fatalf("block must be explicit empty list: %s", output)
	}
	if err := os.WriteFile(filepath.Join(dir, "sources.yaml"), []byte("sources:\n  media: []\n  official:\n"+string(output)), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(conf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(output), "# 粘贴到 sources.official 下\n    - ") || strings.Contains(string(output), "tier:") {
		t.Fatalf("unexpected output: %s", output)
	}
	if len(loaded.Sources) != 1 || loaded.Sources[0].Tier != "official" || loaded.Sources[0].FeedURL != server.URL+"/feed" || loaded.Sources[0].Name != "Fixture" {
		t.Fatalf("loaded sources=%+v", loaded.Sources)
	}
}

func TestSourcesPreviewJSON(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<rss version="2.0"><channel><title>Preview</title><item><title>Raw title</title><link>https://example.com/article</link><pubDate>%s</pubDate></item></channel></rss>`, now.Format(time.RFC1123Z))
	}))
	defer server.Close()
	fetcher := data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}}), nil)
	usecase := biz.NewSourcesUseCase(biz.Sources{{ID: "disabled", Kind: biz.SourceKindRSS, FeedURL: server.URL}}, fetcher, func() time.Time { return now })
	var output bytes.Buffer
	if err := previewSources(t.Context(), usecase, "disabled", &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		ID, Kind string
		Items    []struct {
			Title, URL  string
			PublishedAt string `json:"published_at"`
		}
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != "disabled" || result.Kind != "rss" || len(result.Items) != 1 || result.Items[0].Title != "Raw title" || result.Items[0].URL != "https://example.com/article" || result.Items[0].PublishedAt != "2026-10-27T00:00:00Z" {
		t.Fatal(output.String())
	}
	if err := previewSources(t.Context(), usecase, "unknown", &output); err == nil || err.Error() != "source not found" {
		t.Fatalf("unknown source: %v", err)
	}
}

func TestSourcesPreviewCommandDoesNotOpenStorage(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"config", "sources"} {
		body, err := os.ReadFile("../../configs/" + name + ".example.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	conf := filepath.Join(dir, "config.yaml")
	if err := run([]string{"vane", "sources", "preview", "--conf", conf, "--id", "unknown"}); !errors.Is(err, biz.ErrSourceNotFound) {
		t.Fatalf("preview opened storage or missed unknown ID: %v", err)
	}
	for _, args := range [][]string{
		{"vane", "sources", "preview"},
		{"vane", "sources", "preview", "--conf", conf},
		{"vane", "sources", "preview", "--conf", conf, "--id", "unknown", "extra"},
	} {
		if err := run(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, name := range []string{"config", "sources"} {
		original, err := os.ReadFile("../../configs/" + name + ".example.yaml")
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(dir, name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(original, after) {
			t.Fatal("preview changed configuration")
		}
	}
}
