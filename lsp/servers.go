package lsp

// ServerConfig maps language IDs to LSP server commands.
type ServerConfig struct {
	Command string
	Args    []string
}

// DefaultServers returns built-in language server mappings.
func DefaultServers() map[string]ServerConfig {
	return map[string]ServerConfig{
		"go":         {Command: "gopls"},
		"typescript": {Command: "typescript-language-server", Args: []string{"--stdio"}},
		"javascript": {Command: "typescript-language-server", Args: []string{"--stdio"}},
		"python":     {Command: "pyright-langserver", Args: []string{"--stdio"}},
		"rust":       {Command: "rust-analyzer"},
		"c":          {Command: "clangd"},
		"cpp":        {Command: "clangd"},
		"java":       {Command: "jdtls"},
		"lua":        {Command: "lua-language-server"},
		"json":       {Command: "vscode-json-language-server", Args: []string{"--stdio"}},
	}
}
