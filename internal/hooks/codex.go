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

// CodexWiredFor additionally verifies memsync's hooks point at the current
// binary. It inspects the marker region rather than matching the block verbatim:
// once the user approves the hooks, Codex records trust as [hooks.state] tables
// that it writes INSIDE memsync's region, and a verbatim match would wrongly read
// that as unwired.
func CodexWiredFor(bin string) (bool, error) {
	wired, err := CodexWired()
	if err != nil || !wired {
		return wired, err
	}
	content, err := readText(paths.CodexConfig())
	if err != nil {
		return false, err
	}
	_, inner, _, present, err := splitCodexRegion(content)
	if err != nil || !present {
		return false, err
	}
	return strings.Contains(inner, shellCommand(bin, "inject", "--tool", "codex")) &&
		strings.Contains(inner, shellCommand(bin, "sync", "--tool", "codex")), nil
}

func hasMarkerLine(content, marker string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSuffix(line, "\r") == marker {
			return true
		}
	}
	return false
}

// CodexInstall installs or refreshes memsync's managed block. It is idempotent:
// when the hooks already point at bin it makes no change, so it never disturbs
// the [hooks.state] trust records Codex writes after the user approves the hooks.
// When a refresh is required (no block yet, or a changed binary path) it removes
// only memsync's own tables and preserves any tool-written tables found inside
// the region, re-emitting them after the refreshed block.
func CodexInstall(bin string) error {
	path := paths.CodexConfig()
	if wired, err := CodexWiredFor(bin); err == nil && wired {
		return nil
	}
	content, err := readText(path)
	if err != nil {
		return err
	}
	before, inner, after, present, err := splitCodexRegion(content)
	if err != nil {
		return err
	}
	foreign := ""
	if present {
		foreign = foreignCodexTables(inner)
	}
	if err := backup(path); err != nil {
		return err
	}
	next := strings.TrimRight(before+after, "\n")
	if next != "" {
		next += "\n\n"
	}
	next += codexBlock(bin)
	if foreign = strings.TrimSpace(foreign); foreign != "" {
		next += "\n" + foreign + "\n"
	}
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

// splitCodexRegion divides config content around memsync's managed block into
// the text before the begin marker, the inner lines between the markers, and the
// text after the end marker. present is false when there is no managed block.
// Malformed markers (missing, duplicated, or out of order) are refused so memsync
// never rewrites a config it cannot reason about.
func splitCodexRegion(content string) (before, inner, after string, present bool, err error) {
	const (
		outside = iota
		insideRegion
		afterRegion
	)
	var b, in, a strings.Builder
	state := outside
	begins, ends := 0, 0
	for _, line := range strings.SplitAfter(content, "\n") {
		if line == "" {
			continue
		}
		switch strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") {
		case codexBegin:
			begins++
			if state != outside {
				return "", "", "", false, fmt.Errorf("%s has a nested or duplicate memsync block; refusing to edit", paths.CodexConfig())
			}
			state = insideRegion
			continue
		case codexEnd:
			ends++
			if state != insideRegion {
				return "", "", "", false, fmt.Errorf("%s has an unmatched memsync end marker; refusing to edit", paths.CodexConfig())
			}
			state = afterRegion
			continue
		}
		switch state {
		case outside:
			b.WriteString(line)
		case insideRegion:
			in.WriteString(line)
		default:
			a.WriteString(line)
		}
	}
	if begins == 0 && ends == 0 {
		return content, "", "", false, nil
	}
	if begins != 1 || ends != 1 || state != afterRegion {
		return "", "", "", false, fmt.Errorf("%s has an unterminated memsync block; refusing to edit", paths.CodexConfig())
	}
	return b.String(), in.String(), a.String(), true, nil
}

// stripBlock returns content with memsync's managed block removed.
func stripBlock(content string) (string, error) {
	before, _, after, present, err := splitCodexRegion(content)
	if err != nil {
		return "", err
	}
	if !present {
		return content, nil
	}
	return before + after, nil
}

// foreignCodexTables returns the TOML tables inside memsync's region that memsync
// did not write, most importantly Codex's [hooks.state] hook-trust records. It
// classifies each line by its enclosing table header and keeps everything that is
// not one of memsync's own hook tables, so a refresh preserves tool-written state
// instead of silently discarding it and forcing the user to re-approve the hooks.
func foreignCodexTables(inner string) string {
	var out strings.Builder
	keep := false
	for _, line := range strings.SplitAfter(inner, "\n") {
		if line == "" {
			continue
		}
		header := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
		if isTOMLTableHeader(header) {
			keep = !isMemsyncCodexTable(header)
		}
		if keep {
			out.WriteString(line)
		}
	}
	return out.String()
}

func isTOMLTableHeader(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

// isMemsyncCodexTable reports whether header is one of the array-of-tables that
// codexBlock emits. Keep this in sync with codexBlock.
func isMemsyncCodexTable(header string) bool {
	switch header {
	case "[[hooks.SessionStart]]", "[[hooks.SessionStart.hooks]]",
		"[[hooks.Stop]]", "[[hooks.Stop.hooks]]":
		return true
	}
	return false
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
