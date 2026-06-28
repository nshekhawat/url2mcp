package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/local/url2mcp/internal/store"
)

func TestServeRoot(t *testing.T) {
	chdirToRoot(t)
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServeRootNotFound(t *testing.T) {
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("GET /nonexistent failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleRegisterMethodNotAllowed(t *testing.T) {
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/register")
	if err != nil {
		t.Fatalf("GET /api/register failed: %v", err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestHandleRegisterInvalidJSON(t *testing.T) {
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	body := bytes.NewReader([]byte(`not json`))
	resp, err := http.Post(ts.URL+"/api/register", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/register failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleRegisterMissingFields(t *testing.T) {
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	body := bytes.NewReader([]byte(`{"name":"x"}`))
	resp, err := http.Post(ts.URL+"/api/register", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/register failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleRegisterFetchError(t *testing.T) {
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	payload, _ := json.Marshal(map[string]string{"name": "x", "url": "http://127.0.0.1:1/nonexistent"})
	body := bytes.NewReader(payload)
	resp, err := http.Post(ts.URL+"/api/register", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/register failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestHandleRegisterSuccess(t *testing.T) {
	// Spin up a test HTTP server to serve as the fetch target
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><head><title>Fetched</title></head><body><p>content</p></body></html>`))
	}))
	defer origin.Close()

	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	payload, _ := json.Marshal(map[string]string{"name": "mypage", "url": origin.URL})
	body := bytes.NewReader(payload)
	resp, err := http.Post(ts.URL+"/api/register", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/register failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result registerResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if !result.OK {
		t.Errorf("ok = false, want true: error=%q", result.Error)
	}
	if result.ResourceURI != "url2mcp://mypage" {
		t.Errorf("resource_uri = %q, want %q", result.ResourceURI, "url2mcp://mypage")
	}

	// Verify the resource was stored
	stored, ok := h.store.Get("mypage")
	if !ok {
		t.Fatal("resource not stored")
	}
	if stored.Title != "Fetched" {
		t.Errorf("title = %q, want %q", stored.Title, "Fetched")
	}
}

func TestHandleListResources(t *testing.T) {
	h := handlerWithStore()
	h.store.Set(store.PageContent{Name: "a", URL: "https://a.com"})
	h.store.Set(store.PageContent{Name: "b", URL: "https://b.com"})

	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/resources")
	if err != nil {
		t.Fatalf("GET /api/resources failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var entries []resourceEntry
	json.NewDecoder(resp.Body).Decode(&entries)
	if len(entries) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(entries))
	}
}

func TestHandleListResourcesMethodNotAllowed(t *testing.T) {
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	body := bytes.NewReader([]byte(`{}`))
	resp, err := http.Post(ts.URL+"/api/resources", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/resources failed: %v", err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestHandleResourceByPrefixDelete(t *testing.T) {
	h := handlerWithStore()
	h.store.Set(store.PageContent{Name: "del", URL: "https://del.com"})

	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/resources/del", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/resources/del failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	_, ok := h.store.Get("del")
	if ok {
		t.Fatal("resource not deleted")
	}
}

func TestHandleResourceByPrefixNoName(t *testing.T) {
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/resources/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/resources/ failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandlePreview(t *testing.T) {
	h := handlerWithStore()
	h.store.Set(store.PageContent{
		Name:     "preview",
		URL:      "https://preview.com",
		Title:    "Preview",
		BodyText: "body text",
		Links:    []string{"https://link.com"},
	})

	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/resources/preview/preview")
	if err != nil {
		t.Fatalf("GET /api/resources/preview/preview failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var pr previewResponse
	json.NewDecoder(resp.Body).Decode(&pr)
	if pr.Title != "Preview" {
		t.Errorf("title = %q, want %q", pr.Title, "Preview")
	}
	if pr.BodyText != "body text" {
		t.Errorf("body_text = %q, want %q", pr.BodyText, "body text")
	}
	if len(pr.Links) != 1 {
		t.Errorf("expected 1 link, got %d", len(pr.Links))
	}
}

func TestHandlePreviewNotFound(t *testing.T) {
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/resources/nope/preview")
	if err != nil {
		t.Fatalf("GET /api/resources/nope/preview failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	h := handlerWithStore()
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest("PUT", ts.URL+"/api/resources/x", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/resources/x failed: %v", err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func handlerWithStore() *Handler {
	return NewHandler(store.New())
}

// chdirToRoot changes the working directory to the module root (where go.mod lives)
// so that http.ServeFile can resolve the static/ path relative to the project root.
func chdirToRoot(t *testing.T) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod from " + dir)
		}
		dir = parent
	}
}
