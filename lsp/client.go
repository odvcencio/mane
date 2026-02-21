package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Client manages communication with an LSP server process.
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	mu      sync.Mutex
	nextID  atomic.Int64
	pending map[int64]chan rpcResult
	notify  func(method string, params json.RawMessage)
	closed  atomic.Bool
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewClient starts the LSP server process and returns a Client.
func NewClient(ctx context.Context, command string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		pending: make(map[int64]chan rpcResult),
	}
	go c.readLoop()
	return c, nil
}

// SetNotifyHandler registers a callback for server notifications.
func (c *Client) SetNotifyHandler(fn func(method string, params json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notify = fn
}

func (c *Client) readLoop() {
	defer c.cleanupPending()
	for {
		msg, err := c.readMessage()
		if err != nil {
			return
		}

		if msg.ID != nil {
			c.mu.Lock()
			ch, ok := c.pending[*msg.ID]
			if ok {
				delete(c.pending, *msg.ID)
			}
			c.mu.Unlock()
			if ok {
				if msg.Error != nil {
					ch <- rpcResult{err: fmt.Errorf("rpc error %d: %s", msg.Error.Code, msg.Error.Message)}
				} else {
					ch <- rpcResult{result: msg.Result}
				}
				close(ch)
			}
			continue
		}

		if msg.Method != "" {
			c.mu.Lock()
			fn := c.notify
			c.mu.Unlock()
			if fn != nil {
				fn(msg.Method, msg.Params)
			}
		}
	}
}

func (c *Client) cleanupPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = map[int64]chan rpcResult{}
}

func (c *Client) readMessage() (jsonrpcResponse, error) {
	var contentLength int
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return jsonrpcResponse{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			val := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "content-length:"))
			if n, err := strconv.Atoi(val); err == nil {
				contentLength = n
			}
		}
	}
	if contentLength <= 0 {
		return jsonrpcResponse{}, fmt.Errorf("invalid content-length: %d", contentLength)
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return jsonrpcResponse{}, err
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return jsonrpcResponse{}, err
	}
	return resp, nil
}

func (c *Client) sendMessage(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return fmt.Errorf("client is closed")
	}
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(data)
	return err
}

// Call sends a request and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan rpcResult, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if pending := c.pending[id]; pending != nil {
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}()

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.sendMessage(req); err != nil {
		return nil, err
	}

	select {
	case result, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("client closed")
		}
		if result.err != nil {
			return nil, result.err
		}
		return result.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Notify sends a notification (no response expected).
func (c *Client) Notify(method string, params interface{}) error {
	n := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.sendMessage(n)
}

// DidOpen notifies the server that a document is now open in the editor.
func (c *Client) DidOpen(uri, languageID string, version int, text string) error {
	return c.Notify("textDocument/didOpen", map[string]interface{}{
		"textDocument": TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    version,
			Text:       text,
		},
	})
}

// DidChange notifies the server that a document changed.
func (c *Client) DidChange(uri string, version int, text string) error {
	return c.Notify("textDocument/didChange", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":     uri,
			"version": version,
		},
		"contentChanges": []map[string]interface{}{
			{"text": text},
		},
	})
}

// DidSave notifies the server that a document was saved.
func (c *Client) DidSave(uri string) error {
	return c.Notify("textDocument/didSave", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
	})
}

// DidClose notifies the server that a document is closed.
func (c *Client) DidClose(uri string) error {
	return c.Notify("textDocument/didClose", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
	})
}

func (c *Client) Completion(ctx context.Context, uri string, pos Position) ([]CompletionItem, error) {
	result, err := c.Call(ctx, "textDocument/completion", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
	})
	if err != nil {
		return nil, err
	}

	var list CompletionList
	if err := json.Unmarshal(result, &list); err == nil && len(list.Items) > 0 {
		return list.Items, nil
	}

	var items []CompletionItem
	if err := json.Unmarshal(result, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *Client) Definition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	result, err := c.Call(ctx, "textDocument/definition", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
	})
	if err != nil {
		return nil, err
	}

	var locations []Location
	if err := json.Unmarshal(result, &locations); err == nil && len(locations) > 0 {
		return locations, nil
	}

	var single Location
	if err := json.Unmarshal(result, &single); err != nil {
		return nil, err
	}
	return []Location{single}, nil
}

func (c *Client) References(ctx context.Context, uri string, pos Position) ([]Location, error) {
	result, err := c.Call(ctx, "textDocument/references", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
		"context": map[string]interface{}{
			"includeDeclaration": true,
		},
	})
	if err != nil {
		return nil, err
	}

	var locations []Location
	if err := json.Unmarshal(result, &locations); err != nil {
		return nil, err
	}
	return locations, nil
}

func (c *Client) HoverInfo(ctx context.Context, uri string, pos Position) (*Hover, error) {
	result, err := c.Call(ctx, "textDocument/hover", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
	})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	var hover Hover
	if err := json.Unmarshal(result, &hover); err != nil {
		return nil, err
	}
	return &hover, nil
}

func (c *Client) Rename(ctx context.Context, uri string, pos Position, newName string) (map[string][]TextEdit, error) {
	result, err := c.Call(ctx, "textDocument/rename", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     pos,
		"newName":      newName,
	})
	if err != nil {
		return nil, err
	}
	var response struct {
		Changes map[string][]TextEdit `json:"changes"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, err
	}
	return response.Changes, nil
}

func (c *Client) CodeAction(ctx context.Context, uri string, rng Range, diagnostics []Diagnostic) ([]CodeAction, error) {
	result, err := c.Call(ctx, "textDocument/codeAction", map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"range":        rng,
		"context": map[string]interface{}{
			"diagnostics": diagnostics,
		},
	})
	if err != nil {
		return nil, err
	}
	var actions []CodeAction
	if err := json.Unmarshal(result, &actions); err != nil {
		return nil, err
	}
	return actions, nil
}

// Initialize sends initialize and initialized notifications to the LSP server.
func (c *Client) Initialize(ctx context.Context, rootURI string) error {
	params := map[string]interface{}{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"completion": map[string]interface{}{
					"completionItem": map[string]interface{}{
						"snippetSupport": true,
					},
				},
				"hover":              map[string]interface{}{},
				"definition":         map[string]interface{}{},
				"references":         map[string]interface{}{},
				"rename":             map[string]interface{}{},
				"codeAction":         map[string]interface{}{},
				"publishDiagnostics": map[string]interface{}{},
			},
		},
	}
	if _, err := c.Call(ctx, "initialize", params); err != nil {
		return err
	}
	return c.Notify("initialized", map[string]interface{}{})
}

// Close shuts down the LSP server and clears pending requests.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	c.mu.Lock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	c.mu.Unlock()

	if c.cmd != nil {
		return c.cmd.Wait()
	}
	return nil
}
