// Package cmd is memsync's command-line surface. Dispatch is hand-rolled to keep
// the binary dependency-free and fast to cold-start (it runs on every session hook).
package cmd

import (
	"fmt"
	"os"
)

// Version is overridden at build time via -ldflags.
var Version = "0.1.0-dev"

type command struct {
	name    string
	summary string
	run     func(args []string) int
}

func commands() []command {
	return []command{
		{"init", "Detect tools, wire hooks, make key + local vault, self-test", runInit},
		{"doctor", "Diagnose setup (--fix repairs safe local state)", runDoctor},
		{"status", "Show what's synced right now", runStatus},
		{"sync", "Capture local memories and sync now", runSync},
		{"pair", "Machine 1: add a second machine", runPair},
		{"join", "Machine 2: join an existing vault", runJoin},
		{"remote", "Manage the cross-machine remote (create | set <url>)", runRemote},
		{"uninstall", "Remove memsync's hooks (--purge also clears key/vault)", runUninstall},
		// internal, invoked by hooks:
		{"inject", "", runInject},
		{"guard", "", runGuard},
	}
}

// Execute dispatches argv and returns a process exit code.
func Execute(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage()
		return 0
	}
	if args[0] == "-v" || args[0] == "--version" || args[0] == "version" {
		fmt.Println("memsync", Version)
		return 0
	}
	for _, c := range commands() {
		if c.name == args[0] {
			if hasFlag(args[1:], "-h") || hasFlag(args[1:], "--help") {
				commandUsage(c.name)
				return 0
			}
			return c.run(args[1:])
		}
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
	usage()
	return 2
}

func commandUsage(name string) {
	switch name {
	case "init":
		fmt.Println("Usage: memsync init [--dry-run] [--enable-codex-memories | --no-codex-memories]")
	case "doctor":
		fmt.Println("Usage: memsync doctor [--fix]")
	case "status":
		fmt.Println("Usage: memsync status")
	case "sync":
		fmt.Println("Usage: memsync sync")
	case "pair":
		fmt.Println("Usage: memsync pair [--yes]")
		fmt.Println("  --yes skips the displayed fingerprint confirmation; authenticate the invite another way.")
	case "join":
		fmt.Println("Usage: memsync join")
	case "remote":
		fmt.Println("Usage: memsync remote create | memsync remote set <url>")
	case "uninstall":
		fmt.Println("Usage: memsync uninstall [--purge]")
	default:
		usage()
	}
}

func usage() {
	fmt.Printf("memsync %s - memory courier for Claude Code & Codex\n\n", Version)
	fmt.Println("Usage: memsync <command> [flags]")
	fmt.Println("\nCommands:")
	for _, c := range commands() {
		if c.summary == "" {
			continue
		}
		fmt.Printf("  %-10s %s\n", c.name, c.summary)
	}
	fmt.Println("\nStart with:  memsync init")
}

// --- shared UI helpers ---

func ok(format string, a ...any)   { fmt.Printf("  ✓ "+format+"\n", a...) }
func step(format string, a ...any) { fmt.Printf("\n"+format+"\n", a...) }
func warn(format string, a ...any) { fmt.Printf("  ! "+format+"\n", a...) }
func fail(err error) int           { fmt.Fprintf(os.Stderr, "\nerror: %v\n", err); return 1 }

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
