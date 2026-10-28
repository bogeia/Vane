package data

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/bogeia/vane/internal/biz"
)

type WebListSelectors struct{ Item, Link, Title, Date string }

func parseWebList(body []byte, pageURL string, selectors WebListSelectors) ([]biz.FeedItem, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	compiled := make([]cascadia.Selector, 0, 4)
	for _, selector := range []string{selectors.Item, selectors.Link, selectors.Title, selectors.Date} {
		if strings.TrimSpace(selector) == "" {
			return nil, fmt.Errorf("web list: missing selectors")
		}
		match, err := cascadia.Compile(selector)
		if err != nil {
			return nil, fmt.Errorf("web list: invalid selector")
		}
		compiled = append(compiled, match)
	}
	nodes := cascadia.QueryAll(doc, compiled[0])
	if len(nodes) == 0 {
		return nil, fmt.Errorf("web list: no items matched")
	}
	seen := map[string]bool{}
	items := make([]biz.FeedItem, 0, len(nodes))
	for _, node := range nodes {
		link, title, date := firstMatch(node, compiled[1]), firstMatch(node, compiled[2]), firstMatch(node, compiled[3])
		if link == nil || title == nil || date == nil {
			continue
		}
		href := strings.TrimSpace(attribute(link, "href"))
		if href == "" {
			continue
		}
		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		absolute := base.ResolveReference(ref)
		absolute.Fragment, absolute.RawFragment = "", ""
		if (absolute.Scheme != "http" && absolute.Scheme != "https") || absolute.Hostname() == "" || absolute.User != nil {
			continue
		}
		rawURL := absolute.String()
		if seen[rawURL] {
			continue
		}
		text := nodeText(title)
		if text == "" {
			continue
		}
		dateText := attribute(date, "datetime")
		if dateText == "" {
			dateText = nodeText(date)
		}
		published, ok := webListDate(strings.TrimSpace(dateText))
		if !ok {
			continue
		}
		seen[rawURL] = true
		items = append(items, biz.FeedItem{URL: rawURL, Title: text, Published: &published})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("web list: no item had link, title and date")
	}
	return items, nil
}

func firstMatch(node *html.Node, selector cascadia.Selector) *html.Node {
	if selector.Match(node) {
		return node
	}
	return cascadia.Query(node, selector)
}

func attribute(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return strings.Join(strings.Fields(text.String()), " ")
}

func webListDate(value string) (time.Time, bool) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, true
	}
	beijing := time.FixedZone("CST", 8*60*60)
	for _, layout := range []string{"2006-01-02", "2006/01/02", "2006.01.02", "Jan 2, 2006", "January 2, 2006", "2006年1月2日"} {
		if parsed, err := time.ParseInLocation(layout, value, beijing); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
