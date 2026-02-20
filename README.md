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
| `-web` | | Web server address (e.g. `:8080`) |

### Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+S` | Save file |
| `Ctrl+N` | New file |
| `Ctrl+W` | Close tab |
| `Ctrl+F` | Find in file |
| `Ctrl+B` | Toggle sidebar |
| `Ctrl+P` | Command palette |
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

## Performance

The pure Go tree-sitter runtime benchmarks faster than the C reference implementation via CGo on typical source files, with zero allocation overhead for incremental re-parses.

## License

MIT
