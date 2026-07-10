package hooks

import (
	"os"
	"path/filepath"
	"strings"
)

// shellCommand renders argv as a POSIX shell command. Hook commands are
// strings interpreted by the host tool's shell, so joining argv with spaces
// would break whenever the installed binary path contains whitespace (or a
// shell metacharacter). Single-quoting every argument also keeps the fixed
// memsync arguments literal.
func shellCommand(argv ...string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(arg string) string {
	// End the single-quoted span, emit a literal quote from a double-quoted
	// span, then resume single quoting.
	return "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
}

// writeFileAtomic replaces a tool configuration only after the complete new
// contents have been flushed. Existing permissions are preserved.
func writeFileAtomic(path string, data []byte, defaultMode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := defaultMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
