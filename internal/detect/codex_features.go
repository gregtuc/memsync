package detect

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gregtuc/memsync/internal/paths"
)

// FeatureState is the effective state reported by Codex for a feature.
type FeatureState uint8

const (
	FeatureUnknown FeatureState = iota
	FeatureDisabled
	FeatureEnabled
)

func (s FeatureState) String() string {
	switch s {
	case FeatureDisabled:
		return "disabled"
	case FeatureEnabled:
		return "enabled"
	default:
		return "unknown"
	}
}

// CodexFeatures contains the Codex capabilities memsync depends on. Unknown
// means the installed CLI did not expose a usable `features list` result.
type CodexFeatures struct {
	Memories     FeatureState
	Hooks        FeatureState
	CommandError error
}

// DetectCodexFeatures asks the installed Codex CLI for its effective feature
// states. It is read-only: it never enables a feature or changes config.toml.
//
// Older/broken CLIs may not support `features list`. In that case, an explicit
// hooks=false setting in config.toml is still reported as disabled when it can
// be recognized safely; all other unresolved states remain unknown.
func DetectCodexFeatures() CodexFeatures {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "codex", "features", "list")
	command.WaitDelay = 500 * time.Millisecond
	out, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if len(detail) > 500 {
			detail = detail[:500] + "…"
		}
		if detail == "" {
			detail = err.Error()
		}
		err = fmt.Errorf("codex features list failed: %s", detail)
	}
	return detectCodexFeatures(string(out), err, paths.CodexConfig())
}

func detectCodexFeatures(output string, commandErr error, configPath string) CodexFeatures {
	features := CodexFeatures{Memories: FeatureUnknown, Hooks: FeatureUnknown}
	if commandErr == nil {
		features = parseCodexFeatureList(output)
	} else {
		features.CommandError = commandErr
	}
	if features.Hooks == FeatureUnknown {
		features.Hooks = explicitHooksFeatureState(configPath)
	}
	return features
}

func parseCodexFeatureList(output string) CodexFeatures {
	features := CodexFeatures{Memories: FeatureUnknown, Hooks: FeatureUnknown}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		state := FeatureUnknown
		switch strings.ToLower(fields[len(fields)-1]) {
		case "true":
			state = FeatureEnabled
		case "false":
			state = FeatureDisabled
		default:
			continue
		}
		switch fields[0] {
		case "memories":
			features.Memories = state
		case "hooks":
			features.Hooks = state
		}
	}
	return features
}

func explicitHooksFeatureState(path string) FeatureState {
	b, err := os.ReadFile(path)
	if err != nil {
		return FeatureUnknown
	}
	return parseExplicitHooksFeatureState(string(b))
}

// parseExplicitHooksFeatureState recognizes the ordinary table and dotted-key
// spellings Codex writes. This is intentionally not a general TOML parser: if
// the setting is not an unambiguous boolean, returning unknown is safer than
// guessing about whether hooks can run.
func parseExplicitHooksFeatureState(config string) FeatureState {
	table := ""
	canonical := FeatureUnknown
	legacy := FeatureUnknown
	for _, raw := range strings.Split(config, "\n") {
		line := strings.TrimSpace(stripTOMLComment(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if strings.HasPrefix(line, "[[") {
				table = "<array-table>"
				continue
			}
			table = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		state := parseBooleanFeatureState(value)
		if state == FeatureUnknown {
			continue
		}
		switch {
		case table == "features" && key == "hooks":
			canonical = state
		case table == "features" && key == "codex_hooks":
			legacy = state
		case table == "" && key == "features.hooks":
			canonical = state
		case table == "" && key == "features.codex_hooks":
			legacy = state
		}
	}
	if canonical != FeatureUnknown {
		return canonical
	}
	return legacy
}

func parseBooleanFeatureState(value string) FeatureState {
	switch strings.TrimSpace(value) {
	case "true":
		return FeatureEnabled
	case "false":
		return FeatureDisabled
	default:
		return FeatureUnknown
	}
}

func stripTOMLComment(line string) string {
	inBasicString := false
	inLiteralString := false
	escaped := false
	for i, r := range line {
		switch {
		case inBasicString:
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
			} else if r == '"' {
				inBasicString = false
			}
		case inLiteralString:
			if r == '\'' {
				inLiteralString = false
			}
		case r == '"':
			inBasicString = true
		case r == '\'':
			inLiteralString = true
		case r == '#':
			return line[:i]
		}
	}
	return line
}
