package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

//go:embed static/*
var staticFS embed.FS

// EditorState provides read/write access to the editor's buffer state.
type EditorState interface {
	OpenFile(path string) (string, error)
	ReadBuffer(path string) (string, error)
	WriteBuffer(path string, text string) error
	SaveFile(path string) error
	ListFiles() []string
	GetLanguage(path string) string
}

// Server provides the custom web frontend HTTP + WebSocket server.
type Server struct {
	state    EditorState
	root     string
	upgrader websocket.Upgrader
	mu       sync.Mutex
	clients  []*wsClient
}

type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type rpcRequest struct {
	ID     any             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     any       `json:"id"`
	Result any       `json:"result,omitempty"`
	Error  *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewServer creates a web server backed by the given editor state.
func NewServer(state EditorState, root string) *Server {
	return &Server{
		state: state,
		root:  root,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		s.handleWebSocket(w, r)
		return
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		http.Error(w, "static files unavailable", 500)
		return
	}
	http.FileServer(http.FS(sub)).ServeHTTP(w, r)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	client := &wsClient{conn: conn}
	s.mu.Lock()
	s.clients = append(s.clients, client)
	s.mu.Unlock()

	defer func() {
		conn.Close()
		s.mu.Lock()
		for i, c := range s.clients {
			if c == client {
				s.clients = append(s.clients[:i], s.clients[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var req rpcRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			continue
		}
		resp := s.handleRPC(req)
		data, _ := json.Marshal(resp)
		client.mu.Lock()
		_ = conn.WriteMessage(websocket.TextMessage, data)
		client.mu.Unlock()
	}
}

func (s *Server) handleRPC(req rpcRequest) rpcResponse {
	switch req.Method {
	case "openFile":
		return s.rpcOpenFile(req)
	case "readBuffer":
		return s.rpcReadBuffer(req)
	case "writeBuffer":
		return s.rpcWriteBuffer(req)
	case "saveFile":
		return s.rpcSaveFile(req)
	case "listFiles":
		return s.rpcListFiles(req)
	case "getLanguage":
		return s.rpcGetLanguage(req)
	default:
		return rpcResponse{
			ID:    req.ID,
			Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown method: %s", req.Method)},
		}
	}
}

func (s *Server) rpcOpenFile(req rpcRequest) rpcResponse {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: err.Error()}}
	}
	text, err := s.state.OpenFile(p.Path)
	if err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	lang := s.state.GetLanguage(p.Path)
	return rpcResponse{ID: req.ID, Result: map[string]string{"text": text, "language": lang}}
}

func (s *Server) rpcReadBuffer(req rpcRequest) rpcResponse {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: err.Error()}}
	}
	text, err := s.state.ReadBuffer(p.Path)
	if err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	return rpcResponse{ID: req.ID, Result: map[string]string{"text": text}}
}

func (s *Server) rpcWriteBuffer(req rpcRequest) rpcResponse {
	var p struct {
		Path string `json:"path"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: err.Error()}}
	}
	if err := s.state.WriteBuffer(p.Path, p.Text); err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	return rpcResponse{ID: req.ID, Result: map[string]string{"status": "ok"}}
}

func (s *Server) rpcSaveFile(req rpcRequest) rpcResponse {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: err.Error()}}
	}
	if err := s.state.SaveFile(p.Path); err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	return rpcResponse{ID: req.ID, Result: map[string]string{"status": "saved"}}
}

func (s *Server) rpcListFiles(req rpcRequest) rpcResponse {
	files := s.state.ListFiles()
	return rpcResponse{ID: req.ID, Result: map[string]any{"files": files}}
}

func (s *Server) rpcGetLanguage(req rpcRequest) rpcResponse {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: err.Error()}}
	}
	lang := s.state.GetLanguage(p.Path)
	return rpcResponse{ID: req.ID, Result: map[string]string{"language": lang}}
}

// Broadcast sends a notification to all connected WebSocket clients.
func (s *Server) Broadcast(method string, params any) {
	msg, err := json.Marshal(map[string]any{
		"method": method,
		"params": params,
	})
	if err != nil {
		return
	}
	s.mu.Lock()
	clients := append([]*wsClient(nil), s.clients...)
	s.mu.Unlock()

	for _, c := range clients {
		c.mu.Lock()
		_ = c.conn.WriteMessage(websocket.TextMessage, msg)
		c.mu.Unlock()
	}
}
