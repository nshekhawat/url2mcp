package fetcher

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseHTML(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Test Page</title></head><body><h1>Hello</h1><p>World</p></body></html>`
	pc := parseHTML(html, nil)

	if pc.Title != "Test Page" {
		t.Errorf("title = %q, want %q", pc.Title, "Test Page")
	}
	if !strings.Contains(pc.BodyText, "Hello") || !strings.Contains(pc.BodyText, "World") {
		t.Errorf("body text missing expected content: %q", pc.BodyText)
	}
	if !strings.Contains(pc.BodyHTML, "<h1>") {
		t.Errorf("body HTML missing tags: %q", pc.BodyHTML)
	}
}

func TestParseHTMLNoBody(t *testing.T) {
	html := `<html><head><title>No Body</title></head></html>`
	pc := parseHTML(html, nil)
	if pc.Title != "No Body" {
		t.Errorf("title = %q, want %q", pc.Title, "No Body")
	}
	if pc.BodyHTML != "<body></body>" {
		t.Errorf("expected <body></body>, got %q", pc.BodyHTML)
	}
}

func TestParseHTMLInvalidHTML(t *testing.T) {
	pc := parseHTML("<<<>>>", nil)
	if pc.Title != "" {
		t.Errorf("expected empty title for invalid HTML, got %q", pc.Title)
	}
}

func TestParseHTMLEmpty(t *testing.T) {
	pc := parseHTML("", nil)
	if pc.BodyText != "" {
		t.Errorf("expected empty body text, got %q", pc.BodyText)
	}
}

func TestParseHTMLWithLinks(t *testing.T) {
	html := `<html><body><a href="https://example.com">Example</a><a href="/relative">Relative</a></body></html>`
	base, _ := url.Parse("https://base.com")
	pc := parseHTML(html, base)

	if len(pc.Links) != 2 {
		t.Fatalf("expected 2 links, got %d: %v", len(pc.Links), pc.Links)
	}
	if pc.Links[0] != "https://example.com" {
		t.Errorf("link[0] = %q, want %q", pc.Links[0], "https://example.com")
	}
	if pc.Links[1] != "https://base.com/relative" {
		t.Errorf("link[1] = %q, want %q", pc.Links[1], "https://base.com/relative")
	}
}

func TestParseHTMLSkipJavascriptAndHash(t *testing.T) {
	html := `<html><body><a href="javascript:void(0)">JS</a><a href="#section">Anchor</a><a href="https://ok.com">OK</a></body></html>`
	pc := parseHTML(html, nil)

	if len(pc.Links) != 1 {
		t.Fatalf("expected 1 link, got %d: %v", len(pc.Links), pc.Links)
	}
	if pc.Links[0] != "https://ok.com" {
		t.Errorf("link = %q, want %q", pc.Links[0], "https://ok.com")
	}
}

func TestParseHTMLSkipScriptStyle(t *testing.T) {
	html := `<html><body><script>var x=1;</script><style>.c{}</style><p>visible</p></body></html>`
	pc := parseHTML(html, nil)

	if strings.Contains(pc.BodyText, "var x") {
		t.Errorf("body text should not contain script content: %q", pc.BodyText)
	}
	if strings.Contains(pc.BodyText, ".c{}") {
		t.Errorf("body text should not contain style content: %q", pc.BodyText)
	}
	if !strings.Contains(pc.BodyText, "visible") {
		t.Errorf("body text should contain 'visible': %q", pc.BodyText)
	}
}

func TestExtractLinksDeduplicates(t *testing.T) {
	html := `<html><body><a href="https://x.com">X</a><a href="https://x.com">X again</a></body></html>`
	pc := parseHTML(html, nil)

	if len(pc.Links) != 1 {
		t.Errorf("expected 1 unique link, got %d: %v", len(pc.Links), pc.Links)
	}
}

func TestEscapeHTMLAttr(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"a&b", "a&amp;b"},
		{`a"b`, "a&quot;b"},
		{"a'b", "a&#39;b"},
		{"a<b", "a&lt;b"},
		{"a>b", "a&gt;b"},
		{"a&b<c>", "a&amp;b&lt;c&gt;"},
	}
	for _, tc := range cases {
		var b strings.Builder
		escapeHTMLAttr(&b, tc.input)
		if b.String() != tc.want {
			t.Errorf("escapeHTMLAttr(%q) = %q, want %q", tc.input, b.String(), tc.want)
		}
	}
}

func TestFetchWithTestServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><head><title>TestSrv</title></head><body><p>OK</p></body></html>`))
	}))
	defer ts.Close()

	pc, err := Fetch(ts.URL)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if pc.Title != "TestSrv" {
		t.Errorf("title = %q, want %q", pc.Title, "TestSrv")
	}
	if !strings.Contains(pc.BodyText, "OK") {
		t.Errorf("body text missing 'OK': %q", pc.BodyText)
	}
}

func TestFetchHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	_, err := Fetch(ts.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
}

func TestFetchBadURL(t *testing.T) {
	_, err := Fetch("://invalid-url")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestRenderNodeTextOnly(t *testing.T) {
	html := `<html><body>plain text</body></html>`
	pc := parseHTML(html, nil)
	if !strings.Contains(pc.BodyHTML, "plain text") {
		t.Errorf("body HTML missing text: %q", pc.BodyHTML)
	}
}

func TestExtractTextWithBlockElements(t *testing.T) {
	html := `<html><body><p>First</p><p>Second</p></body></html>`
	pc := parseHTML(html, nil)
	if !strings.Contains(pc.BodyText, "First") || !strings.Contains(pc.BodyText, "Second") {
		t.Errorf("body text missing paragraphs: %q", pc.BodyText)
	}
}

func TestParseHTMLWithNestedElements(t *testing.T) {
	html := `<html><body><div><h1>Title</h1><ul><li>A</li><li>B</li></ul></div></body></html>`
	pc := parseHTML(html, nil)
	if !strings.Contains(pc.BodyText, "Title") {
		t.Errorf("missing Title: %q", pc.BodyText)
	}
	if !strings.Contains(pc.BodyText, "A") || !strings.Contains(pc.BodyText, "B") {
		t.Errorf("missing list items: %q", pc.BodyText)
	}
}
