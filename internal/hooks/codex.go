package hooks

import (
	"fmt"
	"os"
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
	b.WriteString(fmt.Sprintf("command = %q\n\n", shellCommand(bin, "inject", "--tool", "codex")))
	b.WriteString("[[hooks.Stop]]\n\n")
	b.WriteString("[[hooks.Stop.hooks]]\n")
	b.WriteString("type = \"command\"\n")
	b.WriteString(fmt.Sprintf("command = %q\n", shellCommand(bin, "sync", "--tool", "codex")))
	b.WriteString(codexEnd + "\n")
	return b.String()
}

// CodexWired reports whether memsync's managed block is present.
func CodexWired() (bool, error) {
	content, err := readText(paths.CodexConfig())
	if err != nil {
		return false, err
	}
	hasBegin := hasMarkerLine(content, codexBegin)
	hasEnd := hasMarkerLine(content, codexEnd)
	if !hasBegin && !hasEnd {
		return false, nil
	}
	if _, err := stripBlock(content); err != nil {
		return false, err
	}
	if !hasBegin || !hasEnd || strings.Count(content, codexBegin) != 1 || strings.Count(content, codexEnd) != 1 {
		return false, nil
	}
	start := strings.Index(content, codexBegin)
	end := strings.Index(content[start:], codexEnd)
	block := content[start : start+end]
	for _, required := range []string{
		"[[hooks.SessionStart]]",
		"[[hooks.SessionStart.hooks]]",
		"[[hooks.Stop]]",
		"[[hooks.Stop.hooks]]",
		"'inject' '--tool' 'codex'",
		"'sync' '--tool' 'codex'",
	} {
		if !strings.Contains(block, required) {
			return false, nil
		}
	}
	return true, nil
}

// CodexWiredFor additionally verifies the exact current binary path and block.
func CodexWiredFor(bin string) (bool, error) {
	wired, err := CodexWired()
	if err != nil || !wired {
		return wired, err
	}
	content, err := readText(paths.CodexConfig())
	if err != nil {
		return false, err
	}
	return strings.Contains(content, codexBlock(bin)), nil
}

func hasMarkerLine(content, marker string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSuffix(line, "\r") == marker {
			return true
		}
	}
	return false
}

// CodexInstall appends (or refreshes) memsync's managed block. Idempotent.
func CodexInstall(bin string) error {
	path := paths.CodexConfig()
	content, err := readText(path)
	if err != nil {
		return err
	}
	next, err := stripBlock(content)
	if err != nil {
		return err
	}
	if err := backup(path); err != nil {
		return err
	}
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
	next, err := stripBlock(content)
	if err != nil {
		return false, err
	}
	if next == content {
		return false, nil
	}
	if err := backup(path); err != nil {
		return false, err
	}
	return true, writeText(path, strings.TrimRight(next, "\n")+"\n")
}

func stripBlock(content string) (string, error) {
	var out strings.Builder
	inside := false
	for _, line := range strings.SplitAfter(content, "\n") {
		marker := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		switch marker {
		case codexBegin:
			if inside {
				return "", fmt.Errorf("%s has nested memsync begin markers; refusing to edit", paths.CodexConfig())
			}
			inside = true
		case codexEnd:
			if !inside {
				return "", fmt.Errorf("%s has an unmatched memsync end marker; refusing to edit", paths.CodexConfig())
			}
			inside = false
		default:
			if !inside {
				out.WriteString(line)
			}
		}
	}
	if inside {
		return "", fmt.Errorf("%s has an unterminated memsync block; refusing to edit", paths.CodexConfig())
	}
	return out.String(), nil
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
	return writeFileAtomic(path, []byte(content), 0o644)
}
