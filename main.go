package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/odvcencio/fluffyui/fluffy"
	"github.com/odvcencio/mane/editor"
	"github.com/odvcencio/mane/web"
)

func main() {
	webFlag := flag.String("web", "", "web server address for TUI-in-browser (e.g. :8080)")
	webUI := flag.String("webui", "", "custom web UI address (Monaco Editor frontend)")
	mcp := flag.String("mcp", "", "MCP server address (empty = default socket)")
	theme := flag.String("theme", "dark", "theme name")
	flag.Parse()

	args := flag.Args()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Custom web UI mode: serve the Monaco-based frontend.
	if *webUI != "" {
		if err := runWebUI(ctx, args, *webUI); err != nil {
			fmt.Fprintf(os.Stderr, "mane: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Standard TUI mode (optionally with web backend).
	var opts []fluffy.AppOption
	if *webFlag != "" {
		opts = append(opts, fluffy.WithWebServer(*webFlag))
	}
	if *mcp != "" {
		opts = append(opts, fluffy.WithMCP(*mcp))
	}

	if err := run(ctx, args, *theme, opts...); err != nil {
		fmt.Fprintf(os.Stderr, "mane: %v\n", err)
		os.Exit(1)
	}
}

// webUIEditorState adapts the editor.TabManager for the web UI.
type webUIEditorState struct {
	tabs *editor.TabManager
	root string
}

func (s *webUIEditorState) OpenFile(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	idx, err := s.tabs.OpenFile(abs)
	if err != nil {
		return "", err
	}
	s.tabs.SetActive(idx)
	buf := s.tabs.ActiveBuffer()
	if buf == nil {
		return "", fmt.Errorf("failed to open buffer")
	}
	return buf.Text(), nil
}

func (s *webUIEditorState) ReadBuffer(path string) (string, error) {
	for _, buf := range s.tabs.Buffers() {
		if buf.Path() == path {
			return buf.Text(), nil
		}
	}
	return "", fmt.Errorf("buffer not open: %s", path)
}

func (s *webUIEditorState) WriteBuffer(path string, text string) error {
	for _, buf := range s.tabs.Buffers() {
		if buf.Path() == path {
			buf.SetText(text)
			return nil
		}
	}
	return fmt.Errorf("buffer not open: %s", path)
}

func (s *webUIEditorState) SaveFile(path string) error {
	for _, buf := range s.tabs.Buffers() {
		if buf.Path() == path {
			return buf.Save()
		}
	}
	return fmt.Errorf("buffer not open: %s", path)
}

func (s *webUIEditorState) ListFiles() []string {
	var files []string
	_ = filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".claude" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(s.root, path)
		files = append(files, rel)
		return nil
	})
	return files
}

func (s *webUIEditorState) GetLanguage(path string) string {
	return languageIDFromPath(path)
}

func runWebUI(ctx context.Context, paths []string, addr string) error {
	root := ""
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		if info.IsDir() {
			root = abs
			break
		}
		root = filepath.Dir(abs)
	}
	if root == "" {
		root, _ = os.Getwd()
	}

	state := &webUIEditorState{
		tabs: editor.NewTabManager(),
		root: root,
	}

	srv := web.NewServer(state, root)
	server := &http.Server{Addr: addr, Handler: srv}

	go func() {
		<-ctx.Done()
		server.Close()
	}()

	fmt.Printf("Mane web UI: http://localhost%s\n", addr)
	return server.ListenAndServe()
}
