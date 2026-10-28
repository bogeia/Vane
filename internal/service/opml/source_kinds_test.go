package opml

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz"
)

func TestOPMLSourceKindsAndImportSkipsNonRSS(t *testing.T) {
	body, err := Marshal("Vane", []biz.Source{
		{ID: "rss", Kind: biz.SourceKindRSS, Name: "RSS", Tier: "official", FeedURL: "https://example.com/rss"},
		{ID: "web", Kind: biz.SourceKindWebList, Name: "Web", Tier: "official", FeedURL: "https://example.com/news"},
		{ID: "hf", Kind: biz.SourceKindHFModels, Name: "HF", Tier: "official", FeedURL: "https://huggingface.co/org"},
	})
	require.NoError(t, err)
	var document struct {
		Groups []struct {
			Sources []struct {
				Type    string `xml:"type,attr"`
				XMLURL  string `xml:"xmlUrl,attr"`
				HTMLURL string `xml:"htmlUrl,attr"`
			} `xml:"outline"`
		} `xml:"body>outline"`
	}
	require.NoError(t, xml.Unmarshal(body, &document))
	sources := document.Groups[0].Sources
	require.Len(t, sources, 3)
	assert.Equal(t, "rss", sources[0].Type)
	assert.Equal(t, "https://example.com/rss", sources[0].XMLURL)
	assert.Empty(t, sources[0].HTMLURL)
	for i, url := range []string{"https://example.com/news", "https://huggingface.co/org"} {
		assert.Empty(t, sources[i+1].Type)
		assert.Empty(t, sources[i+1].XMLURL)
		assert.Equal(t, url, sources[i+1].HTMLURL)
	}
	parsed, err := Parse(strings.NewReader(string(body)))
	require.NoError(t, err)
	require.Len(t, parsed, 1)
	assert.Equal(t, "https://example.com/rss", parsed[0].URL)
}
