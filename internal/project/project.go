// Package project derives a portable identity for the repository associated
// with a hook invocation. It never sends or stores credentials from remote URLs.
package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Info identifies a project across checkout paths and machines when a Git
// remote is available. Path-based fallback IDs intentionally remain local.
type Info struct {
	ID   string
	Name string
	Root string
}

// Identify resolves cwd to a repository identity without mutating it.
func Identify(cwd string) Info {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cwd, _ = filepath.Abs(cwd)
	// Normalize platform aliases such as macOS' /var -> /private/var so the
	// same checkout cannot acquire two fallback identities depending on whether
	// it came from an argument or os.Getwd().
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	root := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if root == "" {
		root = filepath.Clean(cwd)
	}
	remote := gitOutput(root, "remote", "get-url", "origin")
	identity := normalizeRemote(remote)
	if identity == "" {
		identity = "local:" + root
	}
	sum := sha256.Sum256([]byte(identity))
	name := strings.TrimSuffix(filepath.Base(root), ".git")
	if remoteName := repoName(identity); remoteName != "" && !strings.HasPrefix(identity, "local:") {
		name = remoteName
	}
	return Info{ID: hex.EncodeToString(sum[:8]), Name: name, Root: root}
}

func gitOutput(dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func normalizeRemote(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// SCP-like SSH form: git@host:owner/repo.git
	if !strings.Contains(raw, "://") {
		if at := strings.LastIndex(raw, "@"); at >= 0 {
			raw = raw[at+1:]
		}
		if colon := strings.Index(raw, ":"); colon > 0 {
			return cleanRemoteParts(raw[:colon], raw[colon+1:])
		}
		return cleanRemote(raw)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	return cleanRemoteParts(host, strings.TrimPrefix(u.Path, "/"))
}

func cleanRemote(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(s, "/"))
	s = strings.TrimSuffix(s, ".git")
	return s
}

func cleanRemoteParts(host, path string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	path = cleanRemote(path)
	// GitHub repository paths are case-insensitive; canonicalizing them lets
	// SSH/HTTPS spellings match without conflating case-sensitive generic hosts.
	if host == "github.com" {
		path = strings.ToLower(path)
	}
	return cleanRemote(host + "/" + path)
}

func repoName(identity string) string {
	identity = strings.TrimSuffix(identity, "/")
	if slash := strings.LastIndex(identity, "/"); slash >= 0 {
		return identity[slash+1:]
	}
	return ""
}
