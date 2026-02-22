# Mane: Complete Editor Design

## Overview

Mane is a pure-Go terminal text editor with tree-sitter syntax highlighting. This design covers all remaining features to make it a full-featured editor: core editing power, navigation, LSP integration, web mode, and MCP extensions.

## Current State

Implemented: multi-tab buffers, file open/save, undo/redo, find with match highlighting, syntax highlighting (21+ languages), command palette, file tree sidebar, tab bar (uncommitted), theme system, keyboard shortcuts.

## Section 1: Fix Existing Gaps

- **Commit tab bar** — `tabbar.go` and `app.go` changes are ready
- **Wire replace UI** — Connect `SearchWidget` replace input to `Buffer.Replace()` and `Buffer.ReplaceAll()`, then re-highlight
- **Enable line numbers** — FluffyUI's TextArea supports line numbers; enable the option

## Section 2: Core Editing Power

- **Auto-indent** — On Enter, match current line's indentation. Increase indent after `{`, `(`, `:`. Detect tab vs spaces from file content.
- **Bracket matching** — Use tree-sitter to identify bracket pairs. Highlight matching bracket when cursor is adjacent. Ctrl+Shift+] to jump to match.
- **Line operations** — Ctrl+Shift+K: delete line. Alt+Up/Down: move line. Ctrl+Shift+D: duplicate line. Operate on buffer, trigger re-highlight.
- **Block selection** — Alt+Shift+arrows for rectangular selection. Store as list of ranges.
- **Multiple cursors** — Ctrl+D: select next occurrence. Each cursor is an independent Selection. Edits apply to all cursors. Extend Selection to support multiple ranges.
- **Code folding** — Tree-sitter node boundaries define foldable regions. Ctrl+Shift+[: fold. Ctrl+Shift+]: unfold. Fold gutters in line number area. Folded regions show `...`.
- **Word wrap toggle** — Ctrl+Alt+W toggles. Store preference per buffer or globally.

## Section 3: Navigation & UI

- **Go-to-line** — Ctrl+G opens input overlay. Type line number, Enter to jump.
- **Fuzzy file finder** — Ctrl+P file mode: walk directory tree, fuzzy-match filenames. Display in list. Enter opens file. Reuse CommandPalette or AutoComplete widget.
- **Breadcrumbs** — File path segments below tab bar using FluffyUI's Breadcrumb widget. Click to navigate file tree. Show symbol hierarchy from tree-sitter when available.
- **Status bar enhancements** — Show: encoding (UTF-8), line ending (LF/CRLF), indent mode, language, cursor position (Ln X, Col Y), selection count, Git branch.

## Section 4: LSP Client

New `lsp/` package implementing the Language Server Protocol over stdio.

### Server Configuration

Map language IDs to server commands. Defaults: Go → gopls, TypeScript → typescript-language-server, Python → pyright, Rust → rust-analyzer. Override via config file.

### Features

- **Autocomplete** — Trigger on `.` or Ctrl+Space. Show completion list via AutoComplete/Popover. Support snippets with tab stops.
- **Diagnostics** — Underlined ranges in text area with gutter icons. Diagnostic message on hover or in problems panel.
- **Go-to-definition** — Ctrl+Click or F12. Open target file if needed.
- **Find references** — Shift+F12. Results in list panel, click to navigate.
- **Hover info** — Type/doc info on symbol hover via Tooltip/Popover.
- **Rename symbol** — F2. Input overlay, workspace-wide edits.
- **Code actions** — Ctrl+. for quick fixes and refactorings.

### Document Sync

Send `textDocument/didOpen`, `didChange` (incremental), `didSave`, `didClose`.

## Section 5: Web Mode

### Mode 1: FluffyUI Web Backend (`-web :8080`)

Use FluffyUI's `WithWebServer()` to render the full TUI in browser via xterm.js. No additional work beyond passing the option through.

### Mode 2: Custom Web Frontend (`-web :8080 -webui`)

- HTTP server serves static HTML/CSS/JS
- WebSocket API (JSON-RPC) for editor operations
- Monaco Editor as editing surface
- Native browser UI for file tree and tabs
- v1 scope intentionally focuses on open/edit/save/list + language mode
- Command-palette/LSP parity with TUI is a post-v1 extension target

## Section 6: MCP Extensions

FluffyUI provides 145+ generic MCP tools. Mane adds editor-specific tools and resources.

### Editor Control Tools

- `mane_open_file` — Open file in editor
- `mane_read_buffer` — Read current buffer contents
- `mane_write_buffer` — Replace buffer contents
- `mane_apply_edit` — Apply text edit at range
- `mane_search` — Search across files
- `mane_go_to_line` — Navigate to line
- `mane_get_diagnostics` — Get LSP diagnostics
- `mane_run_command` — Execute command palette command

### Code Intelligence Resources

- `mane://file/{path}` — File contents
- `mane://syntax-tree/{path}` — Tree-sitter parse tree
- `mane://symbols/{path}` — Symbol list
- `mane://diagnostics/{path}` — LSP diagnostics

## Section 7: Testing & Error Handling

- Extend unit test patterns to new packages (`lsp/`, `web/`)
- Use FluffyUI simulation backend for headless integration tests
- LSP server crashes: reconnect with exponential backoff
- File I/O errors: toast notification via FluffyUI's ToastStack
- Web mode disconnections: auto-reconnect with buffered edits

## Section 8: Implementation Order

1. Fix existing gaps (tab bar commit, replace UI, line numbers)
2. Line operations (delete, move, duplicate)
3. Auto-indent and bracket matching
4. Code folding
5. Block selection and multiple cursors
6. Go-to-line and fuzzy file finder
7. Breadcrumbs and status bar enhancements
8. LSP client core (lifecycle, document sync)
9. LSP features (autocomplete, diagnostics, go-to-def, hover, rename, references, code actions)
10. Web mode — FluffyUI backend
11. Web mode — custom frontend with Monaco
12. MCP editor tools and resources

Each step is committed independently.
