package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type e2eMachine struct {
	bin  string
	home string
	cwd  string
	env  []string
}

func (m e2eMachine) run(input string, args ...string) (string, error) {
	cmd := exec.Command(m.bin, args...)
	cmd.Dir = m.cwd
	cmd.Env = m.env
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestEndToEndTwoMachinesPairAndShareBothTools(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "memsync")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/gregtuc/memsync").CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	projectRemote := filepath.Join(root, "project.git")
	vaultRemote := filepath.Join(root, "vault.git")
	fakeGHState := filepath.Join(root, "gh-created")
	claudeScript := "#!/bin/sh\necho 'Claude Code test'\n"
	codexScript := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo 'codex test'
elif [ "$1" = "features" ] && [ "$2" = "list" ]; then
  echo 'hooks stable true'
  echo 'memories experimental true'
else
  exit 0
fi
`
	ghScript := `#!/bin/sh
if [ "$1 $2" = "auth status" ] || [ "$1 $2" = "auth setup-git" ]; then
  exit 0
elif [ "$1 $2" = "api user" ]; then
  echo 'test-user'
elif [ "$1 $2" = "repo view" ]; then
  if [ ! -f "$FAKE_GH_STATE" ]; then
    echo 'GraphQL: Could not resolve to a Repository' >&2
    exit 1
  fi
  printf '{"isPrivate":true,"isEmpty":true,"url":"%s"}\n' "$FAKE_GH_REMOTE"
elif [ "$1 $2" = "repo create" ]; then
  touch "$FAKE_GH_STATE"
else
  exit 1
fi
`
	for name, script := range map[string]string{"claude": claudeScript, "codex": codexScript, "gh": ghScript} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for _, remote := range []string{projectRemote, vaultRemote} {
		runE2EGit(t, root, "init", "--bare", "-q", remote)
		runE2EGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	}
	newMachine := func(name, claudeBody, codexBody string) e2eMachine {
		home := filepath.Join(root, name+"-home")
		cwd := filepath.Join(root, name+"-project")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		runE2EGit(t, root, "clone", "-q", projectRemote, cwd)
		if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = resolved
		}
		encoded := strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-")
		claudeDir := filepath.Join(home, ".claude", "projects", encoded, "memory")
		codexDir := filepath.Join(home, ".codex", "memories")
		for _, dir := range []string{claudeDir, codexDir} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(claudeDir, "shared.md"), []byte(claudeBody+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexDir, "memory_summary.md"), []byte("v1\n"+codexBody+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		env := append(os.Environ(),
			"HOME="+home,
			"CODEX_HOME="+filepath.Join(home, ".codex"),
			"XDG_CONFIG_HOME="+filepath.Join(home, "cfg"),
			"XDG_DATA_HOME="+filepath.Join(home, "data"),
			"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"FAKE_GH_REMOTE="+vaultRemote,
			"FAKE_GH_STATE="+fakeGHState,
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
		)
		return e2eMachine{bin: bin, home: home, cwd: cwd, env: env}
	}
	a := newMachine("a", "CLAUDE-A-CROSS-DEVICE", "CODEX-A-CROSS-DEVICE")
	b := newMachine("b", "CLAUDE-B-LOCAL", "CODEX-B-LOCAL")
	for _, machine := range []e2eMachine{a, b} {
		if out, err := machine.run("", "init"); err != nil {
			t.Fatalf("init %s: %v\n%s", machine.home, err, out)
		}
	}

	join := exec.Command(bin, "join")
	join.Dir = b.cwd
	join.Env = b.env
	joinIn, err := join.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var joinOutput synchronizedBuffer
	join.Stdout = &joinOutput
	join.Stderr = &joinOutput
	if err := join.Start(); err != nil {
		t.Fatal(err)
	}
	joinDone := make(chan error, 1)
	go func() { joinDone <- join.Wait() }()
	invite := waitForToken(t, &joinOutput, joinDone, "msync-invite-", 10*time.Second)
	pairOut, err := a.run(invite+"\nyes\n", "pair")
	if err != nil {
		t.Fatalf("pair: %v\n%s", err, pairOut)
	}
	if !strings.Contains(pairOut, "Reply code:") || !strings.Contains(pairOut, "Private sync is ready") {
		t.Fatalf("pair output did not present the simple happy path:\n%s", pairOut)
	}
	for _, unwanted := range []string{"Sealed reply", "private repo set as origin", "vault pushed"} {
		if strings.Contains(pairOut, unwanted) {
			t.Fatalf("pair output exposed implementation detail %q:\n%s", unwanted, pairOut)
		}
	}
	reply := tokenWithPrefix(pairOut, "msync-reply-")
	if reply == "" {
		t.Fatalf("pair emitted no sealed reply:\n%s", pairOut)
	}
	if _, err := io.WriteString(joinIn, reply+"\n"); err != nil {
		t.Fatal(err)
	}
	_ = joinIn.Close()
	select {
	case err := <-joinDone:
		if err != nil {
			t.Fatalf("join: %v\n%s", err, joinOutput.String())
		}
	case <-time.After(25 * time.Second):
		_ = join.Process.Kill()
		t.Fatalf("join timed out:\n%s", joinOutput.String())
	}
	joinText := joinOutput.String()
	for _, want := range []string{"Connecting this laptop", "This laptop is set up", "When Codex asks, approve memsync to finish"} {
		if !strings.Contains(joinText, want) {
			t.Fatalf("join output missing %q:\n%s", want, joinText)
		}
	}
	for _, unwanted := range []string{"sealed reply", "every record decrypts", "key adopted", "vault cloned", "guards installed", "/hooks"} {
		if strings.Contains(joinText, unwanted) {
			t.Fatalf("join output exposed implementation detail %q:\n%s", unwanted, joinText)
		}
	}
	if _, err := os.Stat(filepath.Join(b.home, "cfg", "memsync", "join-state.json")); !os.IsNotExist(err) {
		t.Fatalf("successful join left temporary pairing state behind: %v", err)
	}

	// Public sync must pull remote-only changes even when A has nothing new.
	if out, err := a.run("", "sync"); err != nil {
		t.Fatalf("public sync did not pull B: %v\n%s", err, out)
	}
	remoteHead := e2eGitOutput(t, root, "--git-dir", vaultRemote, "rev-parse", "refs/heads/main")
	aVault := filepath.Join(a.home, "data", "memsync", "vault")
	if localHead := e2eGitOutput(t, aVault, "rev-parse", "HEAD"); localHead != remoteHead {
		t.Fatalf("public sync left A behind remote: %s != %s", localHead, remoteHead)
	}

	bClaude := hookContext(t, b, "claude")
	for _, want := range []string{"CLAUDE-A-CROSS-DEVICE", "CODEX-A-CROSS-DEVICE"} {
		if !strings.Contains(bClaude, want) {
			t.Fatalf("B Claude missing %s: %q", want, bClaude)
		}
	}
	if strings.Contains(bClaude, "CLAUDE-B-LOCAL") {
		t.Fatalf("B Claude echoed its own local memory: %q", bClaude)
	}

	bCodex := hookContext(t, b, "codex")
	for _, want := range []string{"CLAUDE-A-CROSS-DEVICE", "CODEX-A-CROSS-DEVICE"} {
		if !strings.Contains(bCodex, want) {
			t.Fatalf("B Codex missing %s: %q", want, bCodex)
		}
	}
	if strings.Contains(bCodex, "CODEX-B-LOCAL") {
		t.Fatalf("B Codex echoed its own local memory: %q", bCodex)
	}

	aCodex := hookContext(t, a, "codex")
	if !strings.Contains(aCodex, "CODEX-B-LOCAL") || !strings.Contains(aCodex, "CLAUDE-B-LOCAL") {
		t.Fatalf("A did not receive B's same-tool and cross-tool records: %q", aCodex)
	}

	encodedA := strings.ReplaceAll(filepath.Clean(a.cwd), string(filepath.Separator), "-")
	aClaudeFile := filepath.Join(a.home, ".claude", "projects", encodedA, "memory", "shared.md")
	if err := os.Remove(aClaudeFile); err != nil {
		t.Fatal(err)
	}
	if out, err := a.run("", "sync", "--tool", "claude"); err != nil {
		t.Fatalf("delete sync: %v\n%s", err, out)
	}
	if afterDelete := hookContext(t, b, "codex"); strings.Contains(afterDelete, "CLAUDE-A-CROSS-DEVICE") {
		t.Fatalf("deleted native memory remained shared: %q", afterDelete)
	}

	for _, machine := range []e2eMachine{a, b} {
		if out, err := machine.run("", "doctor"); err != nil {
			t.Fatalf("doctor failed on %s: %v\n%s", machine.home, err, out)
		}
	}
	runE2EGit(t, root, "--git-dir", vaultRemote, "fsck", "--full")
}

func waitForToken(t *testing.T, output *synchronizedBuffer, done <-chan error, prefix string, timeout time.Duration) string {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if token := tokenWithPrefix(output.String(), prefix); token != "" {
			return token
		}
		select {
		case err := <-done:
			t.Fatalf("command exited before emitting %s: %v\n%s", prefix, err, output.String())
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s:\n%s", prefix, output.String())
		case <-ticker.C:
		}
	}
}

func tokenWithPrefix(output, prefix string) string {
	for _, field := range strings.Fields(output) {
		if strings.HasPrefix(field, prefix) {
			return field
		}
	}
	return ""
}

func hookContext(t *testing.T, machine e2eMachine, tool string) string {
	t.Helper()
	out, err := machine.run("", "inject", "--tool", tool)
	if err != nil {
		t.Fatalf("inject %s: %v\n%s", tool, err, out)
	}
	var payload struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); err != nil {
		t.Fatalf("inject %s output is not JSON: %v\n%q", tool, err, out)
	}
	return payload.HookSpecificOutput.AdditionalContext
}

func runE2EGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func e2eGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
