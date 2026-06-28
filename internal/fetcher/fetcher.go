package fetcher

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// PageContent holds the parsed content of a fetched web page.
type PageContent struct {
	Title    string
	BodyHTML string
	BodyText string
	Links    []string
}

// Fetch downloads the given URL, parses the HTML, and returns extracted content.
// It applies a 15-second timeout and a realistic User-Agent header.
func Fetch(rawURL string) (PageContent, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return PageContent{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return PageContent{}, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return PageContent{}, fmt.Errorf("fetch failed: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return PageContent{}, fmt.Errorf("read body: %w", err)
	}

	baseURL, _ := url.Parse(rawURL)
	return parseHTML(string(bodyBytes), baseURL), nil
}

// parseHTML extracts title, body HTML, body text, and links from an HTML document.
func parseHTML(htmlContent string, baseURL *url.URL) PageContent {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return PageContent{}
	}

	var title string
	var bodyNode *html.Node

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if n.FirstChild != nil {
					title = strings.TrimSpace(n.FirstChild.Data)
				}
			case "body":
				bodyNode = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	bodyHTML := ""
	if bodyNode != nil {
		var b strings.Builder
		renderNode(&b, bodyNode)
		bodyHTML = b.String()
	}

	bodyText := ""
	if bodyNode != nil {
		bodyText = extractText(bodyNode)
	}

	links := extractLinks(doc, baseURL)

	return PageContent{
		Title:    title,
		BodyHTML: bodyHTML,
		BodyText: bodyText,
		Links:    links,
	}
}

// renderNode renders an HTML node and its children back to HTML string.
func renderNode(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
	case html.ElementNode:
		b.WriteByte('<')
		b.WriteString(n.Data)
		for _, attr := range n.Attr {
			b.WriteByte(' ')
			b.WriteString(attr.Key)
			if attr.Val != "" {
				b.WriteString(`="`)
				escapeHTMLAttr(b, attr.Val)
				b.WriteByte('"')
			}
		}
		b.WriteByte('>')
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderNode(b, c)
		}
		b.WriteString("</")
		b.WriteString(n.Data)
		b.WriteByte('>')
	case html.DocumentNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderNode(b, c)
		}
	case html.DoctypeNode:
		// skip
	case html.CommentNode:
		// skip
	}
}

func escapeHTMLAttr(b *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
}

// extractText walks the node tree and collects all text content, collapsing whitespace.
func extractText(n *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				parts = append(parts, text)
			}
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript":
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "br", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr", "blockquote", "section", "article":
				parts = append(parts, "\n")
			}
		}
	}
	walk(n)
	return strings.TrimSpace(strings.Join(parts, " "))
}

// extractLinks finds all href attributes on <a> tags and resolves relative URLs.
func extractLinks(doc *html.Node, baseURL *url.URL) []string {
	var links []string
	seen := make(map[string]bool)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" && attr.Val != "" && !strings.HasPrefix(attr.Val, "#") && !strings.HasPrefix(attr.Val, "javascript:") {
					href := attr.Val
					if baseURL != nil {
						if resolved, err := baseURL.Parse(href); err == nil {
							href = resolved.String()
						}
					}
					if !seen[href] {
						seen[href] = true
						links = append(links, href)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return links
}