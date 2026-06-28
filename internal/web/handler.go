package web

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/local/url2mcp/internal/fetcher"
	"github.com/local/url2mcp/internal/store"
)

// Handler serves the web UI and REST API for managing URL resources.
type Handler struct {
	store *store.Store
	mux   *http.ServeMux
}

// NewHandler creates a new web handler with routes registered.
func NewHandler(s *store.Store) *Handler {
	h := &Handler{
		store: s,
		mux:   http.NewServeMux(),
	}
	h.mux.HandleFunc("/", h.handleRoot)
	h.mux.HandleFunc("/api/register", h.handleRegister)
	h.mux.HandleFunc("/api/resources", h.handleListResources)
	h.mux.HandleFunc("/api/resources/", h.handleResourceByPrefix)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "static/index.html")
}

type registerRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type registerResponse struct {
	OK          bool   `json:"ok"`
	ResourceURI string `json:"resource_uri,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, registerResponse{Error: "method not allowed"})
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, registerResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	if req.Name == "" || req.URL == "" {
		writeJSON(w, http.StatusBadRequest, registerResponse{Error: "name and url are required"})
		return
	}

	// Fetch the URL
	parsed, err := fetcher.Fetch(req.URL)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, registerResponse{Error: "fetch failed: " + err.Error()})
		return
	}

	// Store the result
	h.store.Set(store.PageContent{
		Name:      req.Name,
		URL:       req.URL,
		Title:     parsed.Title,
		BodyHTML:  parsed.BodyHTML,
		BodyText:  parsed.BodyText,
		Links:     parsed.Links,
		FetchedAt: time.Now(),
	})

	writeJSON(w, http.StatusOK, registerResponse{
		OK:          true,
		ResourceURI: "url2mcp://" + req.Name,
	})
}

type resourceEntry struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (h *Handler) handleListResources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	entries := h.store.List()
	resources := make([]resourceEntry, 0, len(entries))
	for _, e := range entries {
		resources = append(resources, resourceEntry{Name: e.Name, URL: e.URL})
	}

	writeJSON(w, http.StatusOK, resources)
}

func (h *Handler) handleResourceByPrefix(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Path: /api/resources/{name} or /api/resources/{name}/preview
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/resources/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource name required"})
		return
	}

	name := parts[0]

	switch r.Method {
	case http.MethodGet:
		if len(parts) >= 2 && parts[1] == "preview" {
			h.handlePreview(w, r, name)
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "use /api/resources/{name}/preview"})
		}
	case http.MethodDelete:
		h.store.Delete(name)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

type previewResponse struct {
	Title    string   `json:"title"`
	BodyText string   `json:"body_text"`
	Links    []string `json:"links"`
}

func (h *Handler) handlePreview(w http.ResponseWriter, r *http.Request, name string) {
	pc, ok := h.store.Get(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource not found"})
		return
	}

	writeJSON(w, http.StatusOK, previewResponse{
		Title:    pc.Title,
		BodyText: pc.BodyText,
		Links:    pc.Links,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("error encoding JSON response: %v", err)
	}
}