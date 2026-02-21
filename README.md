# Mane

A terminal text editor built in pure Go with syntax highlighting powered by a custom tree-sitter runtime.

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
| `F1` | Show hover |
| `F2` | Rename symbol |
| `Ctrl+.` | Code actions |
| `Ctrl+Space` | LSP completion |
| `Ctrl+Shift+K` | Delete line |
| `Ctrl+Shift+D` | Duplicate line |
| `Alt+Up` | Move line up |
| `Alt+Down` | Move line down |
| `Ctrl+Alt+W` | Toggle word wrap |
| `Ctrl+B` | Toggle sidebar |
| `Ctrl+Shift+[` | Fold at cursor |
| `Ctrl+Shift+]` | Jump to matching bracket |
| `Ctrl+Z` | Undo |
| `Ctrl+Shift+Z` | Redo |
| `Ctrl+A` | Select all |
| `Shift+Arrow` | Extend selection |
| `Ctrl+Q` | Quit |

## Features

- Syntax highlighting for 21 languages (Go, Python, Rust, TypeScript, C/C++, Java, Ruby, and more)
- Incremental parsing: edits re-highlight only what changed
- Pure Go tree-sitter runtime (no CGo, no C dependencies)
- File tree sidebar with lazy directory loading
- Text selection with clipboard support
- Find with match highlighting and navigation
- Command palette
- Undo/redo
- Line numbers
- Fuzzy file finder (`Ctrl+P`)
- Go-to-line prompt (`Ctrl+G`)
- LSP-powered editor actions:
  - Completion (`Ctrl+Space`)
  - Definition (`F12`)
  - References (`Shift+F12`)
  - Hover (`F1`)
  - Rename (`F2`)
  - Code actions (`Ctrl+.`)
- Multi-cursor:
  - Add next occurrence (`Ctrl+D`)
  - Paste is applied to all active cursors (`Ctrl+V`, multiline paste aware)
  - Mouse click clears multi-cursor mode
  - Insert/Delete across all cursors from the current selection state
- Breadcrumb navigation
- Enhanced status line (encoding, line endings, indent mode, branch, selection)
- Code folding (fold/unfold regions at cursor, fold all, unfold all)
- Block (rectangular) selection
- Web mode:
  - TUI-in-browser via FluffyUI (`-web :8080`)
  - Custom Monaco Editor frontend (`-webui :8080`)
- MCP extensions:
  - Editor control tools (`mane_open_file`, `mane_read_buffer`, `mane_write_buffer`, etc.)
  - Code intelligence resources (`mane://file/`, `mane://symbols/`, `mane://diagnostics/`)

## Performance

The pure Go tree-sitter runtime benchmarks faster than the C reference implementation via CGo on typical source files, with zero allocation overhead for incremental re-parses.

## License

MIT
