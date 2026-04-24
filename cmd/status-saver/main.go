// Command status-saver is the single entry point for the archiver. It
// dispatches to one of three subcommands (run / pair / rotate). See
// `status-saver help` for the full listing.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/ppoloczek/status-saver/internal/buildinfo"
	"github.com/ppoloczek/status-saver/internal/cli/daemon"
	"github.com/ppoloczek/status-saver/internal/cli/pair"
	"github.com/ppoloczek/status-saver/internal/cli/rotate"
)

type subcommand struct {
	summary string
	run     func(args []string) int
}

// Registry of subcommands. Keep summaries short — one line shown in `help`.
var subcommands = map[string]subcommand{
	"run":    {"run the long-running archiver daemon", daemon.Run},
	"pair":   {"interactively pair a WhatsApp account via QR", pair.Run},
	"rotate": {"apply retention and prune old archived files", rotate.Run},
}

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	if sc, ok := subcommands[cmd]; ok {
		os.Exit(sc.run(args))
	}

	switch cmd {
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return
	case "-v", "--version", "version":
		fmt.Println(buildinfo.String())
		return
	}

	fmt.Fprintf(os.Stderr, "status-saver: unknown subcommand %q\n\n", cmd)
	printUsage(os.Stderr)
	os.Exit(2)
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "status-saver — archive WhatsApp status posts from your contacts")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  status-saver <subcommand> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands:")

	names := make([]string, 0, len(subcommands))
	for n := range subcommands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-8s  %s\n", n, subcommands[n].summary)
	}
	fmt.Fprintln(w, "  version   print build information")
	fmt.Fprintln(w, "  help      show this message")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'status-saver <subcommand> --help' for flags.")
}
