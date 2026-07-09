package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregtuc/memsync/internal/paths"
)

const (
	codexBegin = "# >>> memsync (managed) - do not edit this block >>>"
	codexEnd   = "# <<< memsync (managed) <<<"
)

// codexBlock is the marker-delimited TOML memsync appends to config.toml.
// Schema verified against Codex docs: PascalCase event names, and a command
// STRING nested in [[hooks.<Event>.hooks]] with type = "command". The Stop hook
// program emits JSON on exit 0 (Codex requires it). See docs/codex-hooks.md.
//
// We deliberately do NOT emit a [features] table: hooks are enabled by default,
// and a second [features] table would be a duplicate (TOML forbids that) if the
// user already has one, breaking config load. Only array-of-tables ([[...]])
// entries are emitted, which are always safe to append.
func codexBlock(bin string) string {
	var b strings.Builder
	b.WriteString(codexBegin + "\n")
	b.WriteString("[[hooks.SessionStart]]\n\n")
	b.WriteString("[[hooks.SessionStart.hooks]]\n")
	b.WriteString("type = \"command\"\n")
	b.WriteString(fmt.Sprintf("command = %q\n\n", bin+" inject --tool codex"))
	b.WriteString("[[hooks.Stop]]\n\n")
	b.WriteString("[[hooks.Stop.hooks]]\n")
	b.WriteString("type = \"command\"\n")
	b.WriteString(fmt.Sprintf("command = %q\n", bin+" sync --tool codex"))
	b.WriteString(codexEnd + "\n")
	return b.String()
}

// CodexWired reports whether memsync's managed block is present.
func CodexWired() (bool, error) {
	content, err := readText(paths.CodexConfig())
	if err != nil {
		return false, err
	}
	return strings.Contains(content, codexBegin), nil
}

// CodexInstall appends (or refreshes) memsync's managed block. Idempotent.
func CodexInstall(bin string) error {
	path := paths.CodexConfig()
	content, err := readText(path)
	if err != nil {
		return err
	}
	if err := backup(path); err != nil {
		return err
	}
	next := stripBlock(content)
	if next != "" && !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	if next != "" {
		next += "\n"
	}
	next += codexBlock(bin)
	return writeText(path, next)
}

// CodexUninstall removes memsync's managed block. Returns whether anything changed.
func CodexUninstall() (bool, error) {
	path := paths.CodexConfig()
	content, err := readText(path)
	if err != nil || content == "" {
		return false, err
	}
	next := stripBlock(content)
	if next == content {
		return false, nil
	}
	if err := backup(path); err != nil {
		return false, err
	}
	return true, writeText(path, strings.TrimRight(next, "\n")+"\n")
}

func stripBlock(content string) string {
	start := strings.Index(content, codexBegin)
	if start < 0 {
		return content
	}
	end := strings.Index(content, codexEnd)
	if end < 0 {
		return content[:start]
	}
	end += len(codexEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[:start] + content[end:]
}

func readText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func writeText(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
