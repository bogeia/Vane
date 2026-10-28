package opml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
)

type opmlOutline struct {
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr,omitempty"`
	Type     string        `xml:"type,attr,omitempty"`
	URL      string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string        `xml:"htmlUrl,attr,omitempty"`
	Children []opmlOutline `xml:"outline"`
}
type opmlDocument struct {
	XMLName  xml.Name      `xml:"opml"`
	Version  string        `xml:"version,attr"`
	Title    string        `xml:"head>title"`
	Outlines []opmlOutline `xml:"body>outline"`
}

func Parse(input io.Reader) ([]biz.SourceCandidate, error) {
	const limit = 5 << 20
	body, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, fmt.Errorf("OPML exceeds 5 MiB")
	}
	var document opmlDocument
	decoder := xml.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("invalid OPML: %w", err)
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid OPML: %w", err)
		}
		switch token := token.(type) {
		case xml.Comment, xml.ProcInst:
		case xml.CharData:
			if strings.TrimSpace(string(token)) != "" {
				return nil, fmt.Errorf("invalid content after OPML")
			}
		default:
			return nil, fmt.Errorf("invalid content after OPML")
		}
	}
	if document.Version != "1.0" && document.Version != "2.0" {
		return nil, fmt.Errorf("OPML version must be 1.0 or 2.0")
	}
	result := make([]biz.SourceCandidate, 0)
	var visit func([]opmlOutline)
	visit = func(outlines []opmlOutline) {
		for _, outline := range outlines {
			if outline.URL != "" {
				name := outline.Title
				if strings.TrimSpace(name) == "" {
					name = outline.Text
				}
				result = append(result, biz.SourceCandidate{Name: html.UnescapeString(name), URL: outline.URL})
			}
			visit(outline.Children)
		}
	}
	visit(document.Outlines)
	return result, nil
}

func Marshal(siteName string, sources []biz.Source) ([]byte, error) {
	document := opmlDocument{Version: "2.0", Title: siteName + " 信源"}
	for _, group := range []struct{ tier, title string }{{taxonomy.TierOfficial, "官方一手"}, {taxonomy.TierMedia, "媒体与个人"}} {
		outline := opmlOutline{Text: group.title, Title: group.title}
		for _, source := range sources {
			if source.Tier == group.tier {
				child := opmlOutline{Text: source.Name, Title: source.Name}
				if source.Kind == biz.SourceKindRSS {
					child.Type, child.URL = "rss", source.FeedURL
				} else {
					child.HTMLURL = source.FeedURL
				}
				outline.Children = append(outline.Children, child)
			}
		}
		document.Outlines = append(document.Outlines, outline)
	}
	body, err := xml.MarshalIndent(document, "", "  ")
	return append([]byte(xml.Header), body...), err
}
