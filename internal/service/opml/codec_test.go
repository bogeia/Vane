package opml

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseOPMLNestedAndEntities(t *testing.T) {
	sources, err := Parse(strings.NewReader(`<opml version="2.0"><body><outline text="AI"><outline title="Lil&apos;Log 🤖 &amp;amp;" xmlUrl="https://example.com/feed"/></outline></body></opml>`))
	if err != nil || len(sources) != 1 || sources[0].Name != "Lil'Log 🤖 &" {
		t.Fatalf("%+v %v", sources, err)
	}
	for _, input := range []string{`<opml>`, `<opml version="2.0"/><other/>`, `<other/>`, `<opml version="3.0"><body/></opml>`, strings.Repeat(" ", 5<<20) + `<opml version="2.0"/>`} {
		if _, err := Parse(strings.NewReader(input)); err == nil {
			t.Fatal("accepted invalid OPML")
		}
	}
}

func TestParseAwesomeRSSExcerpt(t *testing.T) {
	file, err := os.Open("testdata/awesome-rss-feeds.opml")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	sources, err := Parse(file)
	if err != nil || len(sources) != 3 {
		t.Fatalf("%+v %v", sources, err)
	}
	names := make([]string, 0)
	for _, source := range sources {
		names = append(names, source.Name)
	}
	if !slices.Contains(names, "Lil'Log") || !slices.Contains(names, "📚 Nitay Neeman's Blog | JavaScript • Angular • RxJS") {
		t.Fatal(names)
	}
}
