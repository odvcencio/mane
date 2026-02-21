package mcptools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EditorAccess provides the interface for MCP tools to interact with the editor.
type EditorAccess interface {
	// File operations
	OpenFile(path string) error
	ReadBuffer(path string) (string, error)
	WriteBuffer(path string, text string) error
	ApplyEdit(path string, startLine, startCol, endLine, endCol int, newText string) error
	SaveFile(path string) error

	// Navigation
	GoToLine(line int) error
	GetCursorPosition() (line, col int)

	// Search
	Search(query string) []SearchResult
	SearchFiles(query string, root string) []FileSearchResult

	// LSP
	GetDiagnostics(path string) []DiagnosticInfo

	// Command
	RunCommand(commandID string) error

	// Tree-sitter
	GetSyntaxTree(path string) (string, error)
	GetSymbols(path string) ([]SymbolInfo, error)

	// State
	ActiveFile() string
	ListOpenFiles() []string
	ProjectRoot() string
}

// SearchResult represents a search match in a buffer.
type SearchResult struct {
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Text    string `json:"text"`
	Context string `json:"context"`
}

// FileSearchResult represents a search match across files.
type FileSearchResult struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Text string `json:"text"`
}

// DiagnosticInfo represents an LSP diagnostic for MCP consumption.
type DiagnosticInfo struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

// SymbolInfo represents a code symbol.
type SymbolInfo struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

// ToolDef describes an MCP tool.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Handler     func(params json.RawMessage) (interface{}, error)
}

// ResourceDef describes an MCP resource.
type ResourceDef struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
	Handler     func(uri string) (string, error)
}

// Registry holds all MCP tools and resources for the editor.
type Registry struct {
	editor    EditorAccess
	tools     []ToolDef
	resources []ResourceDef
}

// NewRegistry creates a new MCP registry with all editor tools and resources.
func NewRegistry(editor EditorAccess) *Registry {
	r := &Registry{editor: editor}
	r.registerTools()
	r.registerResources()
	return r
}

// Tools returns all registered MCP tools.
func (r *Registry) Tools() []ToolDef {
	return r.tools
}

// Resources returns all registered MCP resources.
func (r *Registry) Resources() []ResourceDef {
	return r.resources
}

// HandleTool dispatches a tool call by name.
func (r *Registry) HandleTool(name string, params json.RawMessage) (interface{}, error) {
	for _, t := range r.tools {
		if t.Name == name {
			return t.Handler(params)
		}
	}
	return nil, fmt.Errorf("unknown tool: %s", name)
}

// HandleResource dispatches a resource read by URI.
func (r *Registry) HandleResource(uri string) (string, error) {
	for _, res := range r.resources {
		if matchResourceURI(res.URI, uri) {
			return res.Handler(uri)
		}
	}
	return "", fmt.Errorf("unknown resource: %s", uri)
}

// matchResourceURI checks whether a concrete URI matches a resource URI template.
// Templates use {param} placeholders that match one or more path segments.
func matchResourceURI(template, uri string) bool {
	// Find the prefix before the first '{' placeholder.
	idx := strings.Index(template, "{")
	if idx < 0 {
		return template == uri
	}
	prefix := template[:idx]
	return strings.HasPrefix(uri, prefix)
}

// extractURIParam extracts the path parameter from a resource URI given its prefix.
func extractURIParam(prefix, uri string) string {
	return strings.TrimPrefix(uri, prefix)
}

// resolvePath resolves a file path, making it absolute relative to the project root
// if it is not already absolute.
func (r *Registry) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	root := r.editor.ProjectRoot()
	if root == "" {
		// Fallback to working directory.
		root, _ = os.Getwd()
	}
	return filepath.Join(root, path)
}

// registerTools registers all MCP tools with the registry.
func (r *Registry) registerTools() {
	r.tools = []ToolDef{
		r.toolOpenFile(),
		r.toolReadBuffer(),
		r.toolWriteBuffer(),
		r.toolApplyEdit(),
		r.toolSearch(),
		r.toolGoToLine(),
		r.toolGetDiagnostics(),
		r.toolRunCommand(),
	}
}

// registerResources registers all MCP resources with the registry.
func (r *Registry) registerResources() {
	r.resources = []ResourceDef{
		r.resourceFile(),
		r.resourceSyntaxTree(),
		r.resourceSymbols(),
		r.resourceDiagnostics(),
	}
}

// --- Tool definitions ---

func (r *Registry) toolOpenFile() ToolDef {
	return ToolDef{
		Name:        "mane_open_file",
		Description: "Opens a file in the editor. If the file is already open, it becomes the active buffer.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Path to the file to open. Relative paths are resolved against the project root."
				}
			},
			"required": ["path"]
		}`),
		Handler: func(params json.RawMessage) (interface{}, error) {
			var p struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if p.Path == "" {
				return nil, fmt.Errorf("path is required")
			}
			resolved := r.resolvePath(p.Path)
			if err := r.editor.OpenFile(resolved); err != nil {
				return nil, fmt.Errorf("failed to open file: %w", err)
			}
			return map[string]interface{}{
				"path":   resolved,
				"status": "opened",
			}, nil
		},
	}
}

func (r *Registry) toolReadBuffer() ToolDef {
	return ToolDef{
		Name:        "mane_read_buffer",
		Description: "Reads the contents of a file buffer. If no path is provided, reads the active file.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Path to the file to read. If empty, reads the active file."
				}
			}
		}`),
		Handler: func(params json.RawMessage) (interface{}, error) {
			var p struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			path := p.Path
			if path == "" {
				path = r.editor.ActiveFile()
				if path == "" {
					return nil, fmt.Errorf("no active file")
				}
			} else {
				path = r.resolvePath(path)
			}
			content, err := r.editor.ReadBuffer(path)
			if err != nil {
				return nil, fmt.Errorf("failed to read buffer: %w", err)
			}
			return map[string]interface{}{
				"path":    path,
				"content": content,
			}, nil
		},
	}
}

func (r *Registry) toolWriteBuffer() ToolDef {
	return ToolDef{
		Name:        "mane_write_buffer",
		Description: "Writes text to a file buffer, replacing its entire contents. Does not save to disk; use mane_run_command with 'save' to persist.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Path to the file buffer to write."
				},
				"text": {
					"type": "string",
					"description": "The new text content for the buffer."
				}
			},
			"required": ["path", "text"]
		}`),
		Handler: func(params json.RawMessage) (interface{}, error) {
			var p struct {
				Path string `json:"path"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if p.Path == "" {
				return nil, fmt.Errorf("path is required")
			}
			resolved := r.resolvePath(p.Path)
			if err := r.editor.WriteBuffer(resolved, p.Text); err != nil {
				return nil, fmt.Errorf("failed to write buffer: %w", err)
			}
			return map[string]interface{}{
				"path":   resolved,
				"status": "written",
				"length": len(p.Text),
			}, nil
		},
	}
}

func (r *Registry) toolApplyEdit() ToolDef {
	return ToolDef{
		Name:        "mane_apply_edit",
		Description: "Applies a text edit to a specific range in a file buffer. The range is specified by start and end line/column positions (0-indexed). The text in the range is replaced with newText.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Path to the file to edit."
				},
				"startLine": {
					"type": "integer",
					"description": "Start line of the range (0-indexed)."
				},
				"startCol": {
					"type": "integer",
					"description": "Start column of the range (0-indexed)."
				},
				"endLine": {
					"type": "integer",
					"description": "End line of the range (0-indexed)."
				},
				"endCol": {
					"type": "integer",
					"description": "End column of the range (0-indexed)."
				},
				"newText": {
					"type": "string",
					"description": "The replacement text."
				}
			},
			"required": ["path", "startLine", "startCol", "endLine", "endCol", "newText"]
		}`),
		Handler: func(params json.RawMessage) (interface{}, error) {
			var p struct {
				Path      string `json:"path"`
				StartLine int    `json:"startLine"`
				StartCol  int    `json:"startCol"`
				EndLine   int    `json:"endLine"`
				EndCol    int    `json:"endCol"`
				NewText   string `json:"newText"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if p.Path == "" {
				return nil, fmt.Errorf("path is required")
			}
			resolved := r.resolvePath(p.Path)
			if err := r.editor.ApplyEdit(resolved, p.StartLine, p.StartCol, p.EndLine, p.EndCol, p.NewText); err != nil {
				return nil, fmt.Errorf("failed to apply edit: %w", err)
			}
			return map[string]interface{}{
				"path":   resolved,
				"status": "applied",
				"range": map[string]int{
					"startLine": p.StartLine,
					"startCol":  p.StartCol,
					"endLine":   p.EndLine,
					"endCol":    p.EndCol,
				},
			}, nil
		},
	}
}

func (r *Registry) toolSearch() ToolDef {
	return ToolDef{
		Name:        "mane_search",
		Description: "Searches for a query string in the active buffer and returns all matches with their positions and surrounding context.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {
					"type": "string",
					"description": "The search query string."
				}
			},
			"required": ["query"]
		}`),
		Handler: func(params json.RawMessage) (interface{}, error) {
			var p struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if p.Query == "" {
				return nil, fmt.Errorf("query is required")
			}
			results := r.editor.Search(p.Query)
			return map[string]interface{}{
				"query":   p.Query,
				"matches": results,
				"count":   len(results),
			}, nil
		},
	}
}

func (r *Registry) toolGoToLine() ToolDef {
	return ToolDef{
		Name:        "mane_go_to_line",
		Description: "Navigates the cursor to the specified line number in the active file.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"line": {
					"type": "integer",
					"description": "The line number to navigate to (1-indexed)."
				}
			},
			"required": ["line"]
		}`),
		Handler: func(params json.RawMessage) (interface{}, error) {
			var p struct {
				Line int `json:"line"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if p.Line < 1 {
				return nil, fmt.Errorf("line must be >= 1")
			}
			if err := r.editor.GoToLine(p.Line); err != nil {
				return nil, fmt.Errorf("failed to go to line: %w", err)
			}
			line, col := r.editor.GetCursorPosition()
			return map[string]interface{}{
				"line":   line,
				"col":    col,
				"status": "navigated",
			}, nil
		},
	}
}

func (r *Registry) toolGetDiagnostics() ToolDef {
	return ToolDef{
		Name:        "mane_get_diagnostics",
		Description: "Gets LSP diagnostics (errors, warnings, etc.) for a file. If no path is provided, returns diagnostics for the active file.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Path to the file. If empty, uses the active file."
				}
			}
		}`),
		Handler: func(params json.RawMessage) (interface{}, error) {
			var p struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			path := p.Path
			if path == "" {
				path = r.editor.ActiveFile()
				if path == "" {
					return nil, fmt.Errorf("no active file")
				}
			} else {
				path = r.resolvePath(path)
			}
			diagnostics := r.editor.GetDiagnostics(path)
			return map[string]interface{}{
				"path":        path,
				"diagnostics": diagnostics,
				"count":       len(diagnostics),
			}, nil
		},
	}
}

func (r *Registry) toolRunCommand() ToolDef {
	return ToolDef{
		Name:        "mane_run_command",
		Description: "Executes a named editor command (e.g., 'save', 'undo', 'redo', 'format').",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {
					"type": "string",
					"description": "The command ID to execute."
				}
			},
			"required": ["command"]
		}`),
		Handler: func(params json.RawMessage) (interface{}, error) {
			var p struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if p.Command == "" {
				return nil, fmt.Errorf("command is required")
			}
			if err := r.editor.RunCommand(p.Command); err != nil {
				return nil, fmt.Errorf("failed to run command %q: %w", p.Command, err)
			}
			return map[string]interface{}{
				"command": p.Command,
				"status":  "executed",
			}, nil
		},
	}
}

// --- Resource definitions ---

func (r *Registry) resourceFile() ResourceDef {
	return ResourceDef{
		URI:         "mane://file/{path}",
		Name:        "File Contents",
		Description: "Returns the contents of a file buffer or reads from disk if not open.",
		MimeType:    "text/plain",
		Handler: func(uri string) (string, error) {
			path := extractURIParam("mane://file/", uri)
			if path == "" {
				return "", fmt.Errorf("path is required in URI")
			}
			resolved := r.resolvePath(path)
			// Try reading from buffer first.
			content, err := r.editor.ReadBuffer(resolved)
			if err == nil {
				return content, nil
			}
			// Fall back to reading from disk.
			data, err := os.ReadFile(resolved)
			if err != nil {
				return "", fmt.Errorf("failed to read file: %w", err)
			}
			return string(data), nil
		},
	}
}

func (r *Registry) resourceSyntaxTree() ResourceDef {
	return ResourceDef{
		URI:         "mane://syntax-tree/{path}",
		Name:        "Syntax Tree",
		Description: "Returns the tree-sitter parse tree for a file in S-expression format.",
		MimeType:    "text/plain",
		Handler: func(uri string) (string, error) {
			path := extractURIParam("mane://syntax-tree/", uri)
			if path == "" {
				return "", fmt.Errorf("path is required in URI")
			}
			resolved := r.resolvePath(path)
			tree, err := r.editor.GetSyntaxTree(resolved)
			if err != nil {
				return "", fmt.Errorf("failed to get syntax tree: %w", err)
			}
			return tree, nil
		},
	}
}

func (r *Registry) resourceSymbols() ResourceDef {
	return ResourceDef{
		URI:         "mane://symbols/{path}",
		Name:        "Symbols",
		Description: "Returns the list of code symbols (functions, types, variables) in a file as JSON.",
		MimeType:    "application/json",
		Handler: func(uri string) (string, error) {
			path := extractURIParam("mane://symbols/", uri)
			if path == "" {
				return "", fmt.Errorf("path is required in URI")
			}
			resolved := r.resolvePath(path)
			symbols, err := r.editor.GetSymbols(resolved)
			if err != nil {
				return "", fmt.Errorf("failed to get symbols: %w", err)
			}
			data, err := json.Marshal(symbols)
			if err != nil {
				return "", fmt.Errorf("failed to marshal symbols: %w", err)
			}
			return string(data), nil
		},
	}
}

func (r *Registry) resourceDiagnostics() ResourceDef {
	return ResourceDef{
		URI:         "mane://diagnostics/{path}",
		Name:        "Diagnostics",
		Description: "Returns LSP diagnostics for a file as JSON.",
		MimeType:    "application/json",
		Handler: func(uri string) (string, error) {
			path := extractURIParam("mane://diagnostics/", uri)
			if path == "" {
				return "", fmt.Errorf("path is required in URI")
			}
			resolved := r.resolvePath(path)
			diagnostics := r.editor.GetDiagnostics(resolved)
			data, err := json.Marshal(diagnostics)
			if err != nil {
				return "", fmt.Errorf("failed to marshal diagnostics: %w", err)
			}
			return string(data), nil
		},
	}
}
