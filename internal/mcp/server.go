package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/local/url2mcp/internal/store"
)

// jsonRPCRequest represents an incoming JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRPCResponse represents an outgoing JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Server implements the MCP protocol over SSE transport.
type Server struct {
	store   *store.Store
	sessions sync.Map // map[string]chan []byte
	mux     *http.ServeMux
}

// NewServer creates a new MCP server listening on the given address.
func NewServer(s *store.Store) *Server {
	svr := &Server{
		store: s,
		mux:   http.NewServeMux(),
	}
	svr.mux.HandleFunc("/sse", svr.handleSSE)
	svr.mux.HandleFunc("/message", svr.handleMessage)
	svr.mux.HandleFunc("/", svr.handleRoot)
	return svr
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.mux.ServeHTTP(w, r)
}

// handleRoot returns a basic status page for the MCP server root.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"server":   "url2mcp",
		"version":  "0.1.0",
		"protocol": "MCP 2024-11-05",
		"endpoints": []string{
			"GET  /sse      — SSE stream for MCP clients",
			"POST /message  — JSON-RPC message endpoint",
		},
	})
}

// generateSessionID creates a random hex session identifier.
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// fallback — extremely unlikely
		return fmt.Sprintf("sess-%d", sync.Once{})
	}
	return hex.EncodeToString(b)
}

// handleSSE opens an SSE stream for an MCP client.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sessionID := generateSessionID()
	msgCh := make(chan []byte, 64)
	s.sessions.Store(sessionID, msgCh)

	defer func() {
		s.sessions.Delete(sessionID)
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send the endpoint event so the client knows where to POST messages
	endpointData := fmt.Sprintf("/message?sessionId=%s", sessionID)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointData)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", string(msg))
			flusher.Flush()
		}
	}
}

// handleMessage receives JSON-RPC messages from MCP clients.
func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != http.MethodPost {
		writeRPCError(w, nil, -32600, "invalid request: method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		writeRPCError(w, nil, -32600, "invalid request: missing sessionId")
		return
	}

	val, ok := s.sessions.Load(sessionID)
	if !ok {
		writeRPCError(w, nil, -32600, "invalid request: unknown session")
		return
	}
	msgCh, ok := val.(chan []byte)
	if !ok {
		writeRPCError(w, nil, -32600, "invalid request: session error")
		return
	}

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "parse error: "+err.Error())
		return
	}

	// Process the request
	resp := s.processRequest(req)

	// Marshal and send over SSE
	respBytes, err := json.Marshal(resp)
	if err != nil {
		log.Printf("error marshaling response: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	select {
	case msgCh <- respBytes:
	default:
		log.Printf("session %s: channel full, dropping message", sessionID)
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

func (s *Server) processRequest(req jsonRPCRequest) jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		// no-op, acknowledge only
		return jsonRPCResponse{JSONRPC: "2.0"}
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	default:
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32601,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}
}

type initializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    capabilities    `json:"capabilities"`
	ServerInfo      serverInfo      `json:"serverInfo"`
}

type capabilities struct {
	Resources resourceCapability `json:"resources"`
}

type resourceCapability struct {
	Subscribe   bool `json:"subscribe"`
	ListChanged bool `json:"listChanged"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (s *Server) handleInitialize(req jsonRPCRequest) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: initializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: capabilities{
				Resources: resourceCapability{
					Subscribe:   false,
					ListChanged: true,
				},
			},
			ServerInfo: serverInfo{
				Name:    "url2mcp",
				Version: "0.1.0",
			},
		},
	}
}

type resourceItem struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

type resourcesListResult struct {
	Resources []resourceItem `json:"resources"`
}

func (s *Server) handleResourcesList(req jsonRPCRequest) jsonRPCResponse {
	entries := s.store.List()
	resources := make([]resourceItem, 0, len(entries))
	for _, e := range entries {
		resources = append(resources, resourceItem{
			URI:         "url2mcp://" + e.Name,
			Name:        e.Name,
			Description: e.URL,
			MimeType:    "text/plain",
		})
	}

	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: resourcesListResult{
			Resources: resources,
		},
	}
}

type resourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type resourcesReadParams struct {
	URI string `json:"uri"`
}

type resourcesReadResult struct {
	Contents []resourceContent `json:"contents"`
}

func (s *Server) handleResourcesRead(req jsonRPCRequest) jsonRPCResponse {
	var params resourcesReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32602,
				Message: "invalid params: " + err.Error(),
			},
		}
	}

	// Parse the URI: url2mcp://name or url2mcp://name#html or url2mcp://name#links
	uri := params.URI
	if len(uri) < 10 || uri[:10] != "url2mcp://" {
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32602,
				Message: "invalid URI scheme",
			},
		}
	}

	rest := uri[10:]
	resourceName := rest
	subtype := ""

	if idx := stringsIndexByte(rest, '#'); idx >= 0 {
		resourceName = rest[:idx]
		subtype = rest[idx+1:]
	}

	pc, ok := s.store.Get(resourceName)
	if !ok {
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32602,
				Message: "resource not found",
			},
		}
	}

	switch subtype {
	case "html":
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: resourcesReadResult{
				Contents: []resourceContent{
					{
						URI:      uri,
						MimeType: "text/html",
						Text:     pc.BodyHTML,
					},
				},
			},
		}
	case "links":
		linksJSON, _ := json.Marshal(pc.Links)
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: resourcesReadResult{
				Contents: []resourceContent{
					{
						URI:      uri,
						MimeType: "application/json",
						Text:     string(linksJSON),
					},
				},
			},
		}
	default:
		linksJSON, _ := json.Marshal(pc.Links)
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: resourcesReadResult{
				Contents: []resourceContent{
					{
						URI:      "url2mcp://" + resourceName,
						MimeType: "text/plain",
						Text:     pc.BodyText,
					},
					{
						URI:      "url2mcp://" + resourceName + "#html",
						MimeType: "text/html",
						Text:     pc.BodyHTML,
					},
					{
						URI:      "url2mcp://" + resourceName + "#links",
						MimeType: "application/json",
						Text:     string(linksJSON),
					},
				},
			},
		}
	}
}

// stringsIndexByte is a small helper to avoid importing strings just for this.
func stringsIndexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func writeRPCError(w http.ResponseWriter, id interface{}, code int, message string) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	}
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(resp)
}