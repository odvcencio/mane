# Mane

A terminal text editor and code workspace built in pure Go, with syntax highlighting and incremental parsing powered by `gotreesitter` (pure-Go tree-sitter runtime).

Mane focuses on local coding workflows in a terminal UI: multi-tab editing, fast navigation, language-server actions, and web-accessible modes.

## Install

```
go install github.com/odvcencio/mane@latest
```

## Usage

```
mane [flags] [directory-or-file]
```

Open a directory to browse its files, or open a file directly.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-theme` | `dark` | Color theme name |
| `-web` | | Web server address (e.g. `:8080`) for TUI-in-browser |
| `-webui` | | Custom web UI address (Monaco Editor frontend) |
| `-mcp` | | MCP server address |

### Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+S` | Save file |
| `Ctrl+N` | New file |
| `Ctrl+W` | Close tab |
| `Ctrl+P` | Open file |
| `Ctrl+D` | Add next selection occurrence for multi-cursor |
| `Ctrl+V` | Paste at all multi-cursors |
| `Ctrl+F` | Find in file |
| `Ctrl+Shift+P` | Command palette |
| `Ctrl+H` | Replace |
| `Ctrl+G` | Go to line |
| `F12` | Go to definition |
| `Shift+F12` | Find references |
| `F1` | Show hover panel |
| `F8` | Show diagnostics panel |
| `F2` | Rename symbol |
| `Ctrl+.` | Code actions |
| `Ctrl+Space` | LSP completion |
| `Ctrl+Shift+K` | Delete line |
| `Ctrl+Shift+D` | Duplicate line |
| `Alt+Up` | Move line up |
| `Alt+Down` | Move line down |
| `Alt+Shift+Up/Down/Left/Right` | Expand block (rectangular) selection |
| `Ctrl+Alt+W` | Toggle word wrap |
| `Ctrl+B` | Toggle sidebar |
| `Ctrl+Shift+[` | Fold at cursor |
| `Ctrl+Shift+]` | Unfold at cursor |
| `Ctrl+]` | Jump to matching bracket |
| `Esc` | Clear active block/multi-cursor selection mode |
| `Ctrl+Z` | Undo |
| `Ctrl+Shift+Z` | Redo |
| `Ctrl+A` | Select all |
| `Shift+Arrow` | Extend selection |
| `Ctrl+Q` | Quit |

## Features

- Syntax highlighting for 21 languages (Go, Python, Rust, TypeScript, C/C++, Java, Ruby, and more)
- Incremental parsing: edits re-highlight only what changed
- Pure Go tree-sitter runtime (no CGo, no C dependencies)
- Tree-sitter-based fold region detection (with heuristic fallback when unavailable)
- File tree sidebar with lazy directory loading
- Text selection with clipboard support
- Find with match highlighting and navigation
- Command palette
- Undo/redo
- Line numbers
- Fuzzy file finder (`Ctrl+P`)
- Go-to-line prompt (`Ctrl+G`)
- LSP-powered editor actions:
  - Completion (`Ctrl+Space`, snippet tab-stop aware)
  - Definition (`F12`)
  - References (`Shift+F12`)
  - Hover panel (`F1`)
  - Diagnostics panel (`F8`)
  - Rename (`F2`)
  - Code actions (`Ctrl+.`)
- LSP server command overrides from `.mane-lsp.json` (project root), `$XDG_CONFIG_HOME/mane/lsp.json`, or `MANE_LSP_CONFIG`
- Multi-cursor:
  - Add next occurrence (`Ctrl+D`)
  - Paste is applied to all active cursors (`Ctrl+V`, multiline paste aware)
  - Mouse click clears multi-cursor mode
  - Insert/Delete across all cursors from the current selection state
- Breadcrumb navigation (path + current symbol hierarchy when tree-sitter data is available)
- Enhanced status line (encoding, line endings, indent mode, branch, selection)
- Code folding (fold/unfold regions at cursor, fold all/unfold all, folded ranges are hidden from view/navigation)
- Block (rectangular) selection with column-wise insert/delete
- Web mode:
  - TUI-in-browser via FluffyUI (`-web :8080`)
  - Custom Monaco Editor frontend (`-webui :8080`) for open/edit/save/list workflows
- MCP extensions (`-mcp`):
  - Editor tools (`mane_open_file`, `mane_read_buffer`, `mane_write_buffer`, `mane_apply_edit`, `mane_search`, `mane_go_to_line`, `mane_get_diagnostics`, `mane_run_command`)
  - Code intelligence resources (`mane://file/{path}`, `mane://syntax-tree/{path}`, `mane://symbols/{path}`, `mane://diagnostics/{path}`)

## v1 Scope

Mane v1 targets full-featured terminal editing workflows and exposes MCP + web modes for integration and remote operation.

- Terminal UI is the primary feature-complete surface.
- `-webui` (Monaco frontend) intentionally focuses on file open/edit/save/list flows via RPC rather than mirroring every terminal command.

## Performance

The pure Go tree-sitter runtime benchmarks faster than the C reference implementation via CGo on typical source files, with zero allocation overhead for incremental re-parses.

## License

MIT
