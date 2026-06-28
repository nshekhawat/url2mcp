package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const maxSSETokenSize = 10 * 1024 * 1024 // 10 MB

func main() {
	server := flag.String("server", "http://localhost:8081", "MCP server base URL")
	resource := flag.String("resource", "", "resource name to read")
	subtype := flag.String("subtype", "", "resource subtype: text, html, or links (default: all)")
	list := flag.Bool("list", false, "list all available resources")
	flag.Parse()

	if !*list && *resource == "" {
		fmt.Fprintln(os.Stderr, "error: specify --resource <name> or --list")
		flag.Usage()
		os.Exit(1)
	}

	client := &mcpClient{baseURL: *server}
	if err := client.run(*resource, *subtype, *list); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

type mcpClient struct {
	baseURL   string
	httpClient *http.Client
}

func (c *mcpClient) run(resource, subtype string, list bool) error {
	c.httpClient = &http.Client{}

	sessionID, resp, err := c.connectSSE()
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Close()

	respCh := make(chan json.RawMessage, 16)
	errCh := make(chan error, 1)
	go readSSE(resp, respCh, errCh)

	if err := c.sendRequest(sessionID, 1, "initialize", nil); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if err := waitForResponse(respCh, errCh, 1); err != nil {
		return fmt.Errorf("initialize response: %w", err)
	}

	if list {
		return c.listResources(sessionID, respCh, errCh)
	}

	return c.readResource(sessionID, resource, subtype, respCh, errCh)
}

func (c *mcpClient) connectSSE() (string, io.ReadCloser, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/sse", nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return "", nil, fmt.Errorf("SSE status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, maxSSETokenSize), maxSSETokenSize)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if strings.Contains(data, "sessionId=") {
				return extractSessionID(data), resp.Body, nil
			}
		}
	}
	resp.Body.Close()
	return "", nil, fmt.Errorf("no endpoint event received")
}

func extractSessionID(data string) string {
	idx := strings.Index(data, "sessionId=")
	if idx < 0 {
		return ""
	}
	rest := data[idx+len("sessionId="):]
	if sp := strings.IndexAny(rest, "& \t\n"); sp >= 0 {
		return rest[:sp]
	}
	return rest
}

func (c *mcpClient) sendRequest(sessionID string, id int, method string, params interface{}) error {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/message?sessionId=%s", c.baseURL, sessionID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: HTTP %d: %s", method, resp.StatusCode, string(bodyBytes))
	}
	return nil
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func waitForResponse(respCh chan json.RawMessage, errCh chan error, expectedID int) error {
	for {
		select {
		case msg := <-respCh:
			var rpc rpcResponse
			if err := json.Unmarshal(msg, &rpc); err != nil {
				continue
			}
			var id int
			if err := json.Unmarshal(rpc.ID, &id); err != nil {
				continue
			}
			if id != expectedID {
				continue
			}
			if rpc.Error != nil {
				return fmt.Errorf("RPC error %d: %s", rpc.Error.Code, rpc.Error.Message)
			}
			return nil
		case err := <-errCh:
			return err
		}
	}
}

func (c *mcpClient) listResources(sessionID string, respCh chan json.RawMessage, errCh chan error) error {
	if err := c.sendRequest(sessionID, 2, "resources/list", nil); err != nil {
		return err
	}

	for {
		select {
		case msg := <-respCh:
			var rpc rpcResponse
			if err := json.Unmarshal(msg, &rpc); err != nil {
				continue
			}
			var id int
			if err := json.Unmarshal(rpc.ID, &id); err != nil {
				continue
			}
			if id != 2 {
				continue
			}
			if rpc.Error != nil {
				return fmt.Errorf("RPC error %d: %s", rpc.Error.Code, rpc.Error.Message)
			}

			var result struct {
				Resources []struct {
					URI         string `json:"uri"`
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"resources"`
			}
			if err := json.Unmarshal(rpc.Result, &result); err != nil {
				return fmt.Errorf("parse list result: %w", err)
			}
			if len(result.Resources) == 0 {
				fmt.Println("No resources registered.")
				return nil
			}
			for _, r := range result.Resources {
				fmt.Printf("%-20s %s\n", r.Name, r.Description)
			}
			return nil
		case err := <-errCh:
			return err
		}
	}
}

func (c *mcpClient) readResource(sessionID, resource, subtype string, respCh chan json.RawMessage, errCh chan error) error {
	uri := fmt.Sprintf("url2mcp://%s", resource)
	if subtype != "" {
		uri = fmt.Sprintf("url2mcp://%s#%s", resource, subtype)
	}

	if err := c.sendRequest(sessionID, 2, "resources/read", map[string]string{"uri": uri}); err != nil {
		return err
	}

	for {
		select {
		case msg := <-respCh:
			var rpc rpcResponse
			if err := json.Unmarshal(msg, &rpc); err != nil {
				continue
			}
			var id int
			if err := json.Unmarshal(rpc.ID, &id); err != nil {
				continue
			}
			if id != 2 {
				continue
			}
			if rpc.Error != nil {
				return fmt.Errorf("RPC error %d: %s", rpc.Error.Code, rpc.Error.Message)
			}

			var result struct {
				Contents []struct {
					URI      string `json:"uri"`
					MimeType string `json:"mimeType"`
					Text     string `json:"text"`
				} `json:"contents"`
			}
			if err := json.Unmarshal(rpc.Result, &result); err != nil {
				return fmt.Errorf("parse read result: %w", err)
			}

			for _, c := range result.Contents {
				fmt.Printf("--- %s (%s) ---\n", c.URI, c.MimeType)
				fmt.Println(c.Text)
			}
			return nil
		case err := <-errCh:
			return err
		}
	}
}

func readSSE(body io.Reader, respCh chan<- json.RawMessage, errCh chan<- error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, maxSSETokenSize), maxSSETokenSize)
	var dataBuf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataBuf.WriteString(strings.TrimPrefix(line, "data: "))
		} else if line == "" && dataBuf.Len() > 0 {
			data := dataBuf.String()
			dataBuf.Reset()

			if strings.HasPrefix(data, "/message?") {
				continue
			}

			var raw json.RawMessage
			if err := json.Unmarshal([]byte(data), &raw); err == nil {
				respCh <- raw
			}
		} else {
			if line == "" {
				dataBuf.Reset()
			}
		}
	}

	if err := scanner.Err(); err != nil {
		errCh <- err
	}
	close(respCh)
}