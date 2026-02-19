package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/odvcencio/fluffyui/fluffy"
)

func main() {
	web := flag.String("web", "", "web server address (e.g. :8080)")
	mcp := flag.String("mcp", "", "MCP server address (empty = default socket)")
	theme := flag.String("theme", "dark", "theme name")
	flag.Parse()

	// Use first positional arg as root directory, default to cwd.
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	var opts []fluffy.AppOption
	if *web != "" {
		opts = append(opts, fluffy.WithWebServer(*web))
	}
	if *mcp != "" {
		opts = append(opts, fluffy.WithMCP(*mcp))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, root, *theme, opts...); err != nil {
		fmt.Fprintf(os.Stderr, "mane: %v\n", err)
		os.Exit(1)
	}
}
