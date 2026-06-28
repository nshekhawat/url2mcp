package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/local/url2mcp/internal/mcp"
	"github.com/local/url2mcp/internal/store"
)

func TestExtractSessionID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/message?sessionId=abc123", "abc123"},
		{"/message?sessionId=abc123&other=val", "abc123"},
		{"sessionId=xyz", "xyz"},
		{"no session here", ""},
	}
	for _, tc := range tests {
		got := extractSessionID(tc.input)
		if got != tc.want {
			t.Errorf("extractSessionID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestMCPServerRoot(t *testing.T) {
	s := mcp.NewServer(store.New())
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["server"] != "url2mcp" {
		t.Errorf("server = %v, want url2mcp", result["server"])
	}
}

func TestMCPClientReadResource(t *testing.T) {
	st := store.New()
	st.Set(store.PageContent{
		Name:     "testpage",
		URL:      "https://example.com",
		Title:    "Test Title",
		BodyText: "Hello World Content",
		BodyHTML: "<p>Hello World Content</p>",
		Links:    []string{"https://example.com/link1"},
	})

	s := mcp.NewServer(st)
	ts := httptest.NewServer(s)
	defer ts.Close()

	client := &mcpClient{baseURL: ts.URL, httpClient: &http.Client{}}
	if err := client.run("testpage", "", false); err != nil {
		t.Fatalf("client run failed: %v", err)
	}
}

func TestMCPClientReadResourceSubtype(t *testing.T) {
	st := store.New()
	st.Set(store.PageContent{
		Name:     "testpage",
		URL:      "https://example.com",
		BodyHTML: "<b>bold</b>",
	})

	s := mcp.NewServer(st)
	ts := httptest.NewServer(s)
	defer ts.Close()

	client := &mcpClient{baseURL: ts.URL, httpClient: &http.Client{}}
	if err := client.run("testpage", "html", false); err != nil {
		t.Fatalf("client run failed for html subtype: %v", err)
	}
}

func TestMCPClientListResources(t *testing.T) {
	st := store.New()
	st.Set(store.PageContent{Name: "a", URL: "https://a.com"})
	st.Set(store.PageContent{Name: "b", URL: "https://b.com"})

	s := mcp.NewServer(st)
	ts := httptest.NewServer(s)
	defer ts.Close()

	client := &mcpClient{baseURL: ts.URL, httpClient: &http.Client{}}
	if err := client.run("", "", true); err != nil {
		t.Fatalf("client list failed: %v", err)
	}
}

func TestMCPClientResourceNotFound(t *testing.T) {
	st := store.New()
	s := mcp.NewServer(st)
	ts := httptest.NewServer(s)
	defer ts.Close()

	client := &mcpClient{baseURL: ts.URL, httpClient: &http.Client{}}
	err := client.run("nonexistent", "", false)
	if err == nil {
		t.Fatal("expected error for missing resource")
	}
	if !strings.Contains(err.Error(), "resource not found") {
		t.Errorf("error = %q, want resource not found", err.Error())
	}
}

func TestReadSSE(t *testing.T) {
	input := `event: endpoint
data: /message?sessionId=abc

data: {"jsonrpc":"2.0","id":1,"result":{"ok":true}}

data: {"jsonrpc":"2.0","id":2,"result":{"items":[]}}

`

	respCh := make(chan json.RawMessage, 4)
	errCh := make(chan error, 1)

	go readSSE(strings.NewReader(input), respCh, errCh)

	// Should get 2 JSON messages, skip the endpoint event
	msg1 := <-respCh
	var rpc1 struct {
		ID int `json:"id"`
	}
	json.Unmarshal(msg1, &rpc1)
	if rpc1.ID != 1 {
		t.Errorf("first msg id = %d, want 1", rpc1.ID)
	}

	msg2 := <-respCh
	var rpc2 struct {
		ID int `json:"id"`
	}
	json.Unmarshal(msg2, &rpc2)
	if rpc2.ID != 2 {
		t.Errorf("second msg id = %d, want 2", rpc2.ID)
	}

	select {
	case _, ok := <-respCh:
		if ok {
			t.Error("unexpected extra message")
		}
	default:
	}
}
