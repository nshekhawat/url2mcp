package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/local/url2mcp/internal/store"
)

func newID(id int) json.RawMessage {
	b, _ := json.Marshal(id)
	return b
}

func TestProcessRequestInitialize(t *testing.T) {
	s := mcpServerWithStore()
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "initialize",
	}
	resp := s.processRequest(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected result")
	}

	result, ok := resp.Result.(initializeResult)
	if !ok {
		t.Fatalf("result type = %T, want initializeResult", resp.Result)
	}
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("protocolVersion = %q, want %q", result.ProtocolVersion, "2024-11-05")
	}
	if result.ServerInfo.Name != "url2mcp" {
		t.Errorf("server name = %q, want %q", result.ServerInfo.Name, "url2mcp")
	}
}

func TestProcessRequestNotificationsInitialized(t *testing.T) {
	s := mcpServerWithStore()
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	resp := s.processRequest(req)
	if resp.Error != nil {
		t.Errorf("unexpected error: %+v", resp.Error)
	}
}

func TestProcessRequestResourcesList(t *testing.T) {
	s := mcpServerWithStore()
	s.store.Set(store.PageContent{Name: "foo", URL: "https://foo.com"})

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "resources/list",
	}
	resp := s.processRequest(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	rlr, ok := resp.Result.(resourcesListResult)
	if !ok {
		t.Fatalf("result type = %T", resp.Result)
	}
	if len(rlr.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(rlr.Resources))
	}
	if rlr.Resources[0].Name != "foo" {
		t.Errorf("resource name = %q, want %q", rlr.Resources[0].Name, "foo")
	}
}

func TestProcessRequestResourcesListEmpty(t *testing.T) {
	s := mcpServerWithStore()
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "resources/list",
	}
	resp := s.processRequest(req)
	rlr := resp.Result.(resourcesListResult)
	if len(rlr.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(rlr.Resources))
	}
}

func TestProcessRequestResourcesRead(t *testing.T) {
	s := mcpServerWithStore()
	s.store.Set(store.PageContent{Name: "test", BodyText: "hello world", BodyHTML: "<p>hello</p>", Links: []string{"https://a.com"}})

	params, _ := json.Marshal(map[string]string{"uri": "url2mcp://test"})
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "resources/read",
		Params:  params,
	}
	resp := s.processRequest(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	rrr := resp.Result.(resourcesReadResult)
	if len(rrr.Contents) != 3 {
		t.Fatalf("expected 3 content entries, got %d", len(rrr.Contents))
	}
}

func TestProcessRequestResourcesReadHTML(t *testing.T) {
	s := mcpServerWithStore()
	s.store.Set(store.PageContent{Name: "p", BodyHTML: "<b>bold</b>"})

	params, _ := json.Marshal(map[string]string{"uri": "url2mcp://p#html"})
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "resources/read",
		Params:  params,
	}
	resp := s.processRequest(req)
	rrr := resp.Result.(resourcesReadResult)
	if len(rrr.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(rrr.Contents))
	}
	if rrr.Contents[0].MimeType != "text/html" {
		t.Errorf("mime = %q, want text/html", rrr.Contents[0].MimeType)
	}
}

func TestProcessRequestResourcesReadLinks(t *testing.T) {
	s := mcpServerWithStore()
	s.store.Set(store.PageContent{Name: "p", Links: []string{"https://x.com"}})

	params, _ := json.Marshal(map[string]string{"uri": "url2mcp://p#links"})
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "resources/read",
		Params:  params,
	}
	resp := s.processRequest(req)
	rrr := resp.Result.(resourcesReadResult)
	if rrr.Contents[0].MimeType != "application/json" {
		t.Errorf("mime = %q, want application/json", rrr.Contents[0].MimeType)
	}
}

func TestProcessRequestResourcesReadNotFound(t *testing.T) {
	s := mcpServerWithStore()
	params, _ := json.Marshal(map[string]string{"uri": "url2mcp://nope"})
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "resources/read",
		Params:  params,
	}
	resp := s.processRequest(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing resource")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestProcessRequestResourcesReadInvalidURI(t *testing.T) {
	s := mcpServerWithStore()
	params, _ := json.Marshal(map[string]string{"uri": "invalid"})
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "resources/read",
		Params:  params,
	}
	resp := s.processRequest(req)
	if resp.Error == nil {
		t.Fatal("expected error for invalid URI")
	}
}

func TestProcessRequestResourcesReadBadParams(t *testing.T) {
	s := mcpServerWithStore()
	params := json.RawMessage(`{"uri":123}`)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "resources/read",
		Params:  params,
	}
	resp := s.processRequest(req)
	if resp.Error == nil {
		t.Fatal("expected error for bad params")
	}
}

func TestProcessRequestUnknownMethod(t *testing.T) {
	s := mcpServerWithStore()
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      newID(1),
		Method:  "unknown",
	}
	resp := s.processRequest(req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestServeHTTPCORS(t *testing.T) {
	s := mcpServerWithStore()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// OPTIONS preflight
	req, _ := http.NewRequest("OPTIONS", ts.URL+"/sse", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("OPTIONS status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("missing CORS header")
	}
}

func TestHandleMessageMethodNotAllowed(t *testing.T) {
	s := mcpServerWithStore()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/message")
	if err != nil {
		t.Fatalf("GET /message failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleMessageMissingSessionID(t *testing.T) {
	s := mcpServerWithStore()
	ts := httptest.NewServer(s)
	defer ts.Close()

	body := bytes.NewReader([]byte(`{}`))
	resp, err := http.Post(ts.URL+"/message", "application/json", body)
	if err != nil {
		t.Fatalf("POST /message failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleMessageParseError(t *testing.T) {
	s := mcpServerWithStore()

	sessionID := "test-session"
	msgCh := make(chan []byte, 64)
	s.sessions.Store(sessionID, msgCh)
	defer s.sessions.Delete(sessionID)

	ts := httptest.NewServer(s)
	defer ts.Close()

	body := bytes.NewReader([]byte(`not json`))
	resp, err := http.Post(ts.URL+"/message?sessionId="+sessionID, "application/json", body)
	if err != nil {
		t.Fatalf("POST /message failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleMessageSuccess(t *testing.T) {
	s := mcpServerWithStore()

	sessionID := "test-session-success"
	msgCh := make(chan []byte, 64)
	s.sessions.Store(sessionID, msgCh)
	defer s.sessions.Delete(sessionID)

	ts := httptest.NewServer(s)
	defer ts.Close()

	rpcReq, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})

	body := bytes.NewReader(rpcReq)
	resp, err := http.Post(ts.URL+"/message?sessionId="+sessionID, "application/json", body)
	if err != nil {
		t.Fatalf("POST /message failed: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}

	select {
	case msg := <-msgCh:
		if !strings.Contains(string(msg), "url2mcp") {
			t.Errorf("response missing server info: %s", string(msg))
		}
	default:
		t.Fatal("expected message on session channel")
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()
	if id1 == id2 {
		t.Error("session IDs should be unique")
	}
	if len(id1) != 32 {
		t.Errorf("expected 32-char hex, got %d chars: %s", len(id1), id1)
	}
}

func TestHandleRoot(t *testing.T) {
	s := mcpServerWithStore()
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
		t.Fatalf("decode: %v", err)
	}
	if result["server"] != "url2mcp" {
		t.Errorf("server = %v, want url2mcp", result["server"])
	}
}

func TestHandleRootNotFound(t *testing.T) {
	s := mcpServerWithStore()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("GET /nonexistent failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func mcpServerWithStore() *Server {
	return NewServer(store.New())
}
