# Codex integration

memsync uses two user-scope lifecycle hooks in `~/.codex/config.toml`:

```toml
# >>> memsync (managed) - do not edit this block >>>
[[hooks.SessionStart]]

[[hooks.SessionStart.hooks]]
type = "command"
command = "/absolute/path/to/memsync inject --tool codex"

[[hooks.Stop]]

[[hooks.Stop.hooks]]
type = "command"
command = "/absolute/path/to/memsync sync --tool codex"
# <<< memsync (managed) <<<
```

`SessionStart` refreshes the encrypted vault and adds relevant shared memory as
developer context. `Stop` captures Codex's generated memory and returns the JSON
object Codex expects from that event. Both hook commands fail open so a memsync
problem does not prevent a Codex session from continuing.

The command in the real block is shell-quoted, so an installed path containing
spaces or shell metacharacters remains safe. `memsync init` is idempotent and
refreshes only its marker-delimited block; `memsync uninstall` removes that same
block.

## Hook trust

Codex requires users to review and trust each new or changed non-managed command
hook. Merely finding the block in `config.toml` does not prove that Codex ran it.
After setup:

1. Open Codex.
2. Choose **Review hooks**, or enter `/hooks`.
3. Inspect and trust both memsync commands.
4. Start a fresh session, then run `memsync status` to confirm the hook was
   observed.

Trust is recorded against the exact definition. Moving the memsync binary or
changing the command can require another review. `memsync doctor --fix` refreshes
stale paths; review the hooks again afterward.

## Feature flags

Codex hooks are enabled by default. If the effective `hooks` feature is
explicitly disabled, `memsync init` enables it and `memsync doctor` reports a
remaining problem.

Codex **Memories** is a separate feature used when Codex contributes context to
Claude or another laptop. `memsync init` enables it automatically as part of
connecting Codex; no separate setup choice is required.

Codex generates memory in the background after eligible work becomes idle, so
the supported files under `~/.codex/memories/` may not appear immediately. A
successful hook with zero Codex records can therefore be normal at first. Use
`memsync status`, then `memsync sync` after Codex has generated a summary.

## Schema details

- Event names are case-sensitive: `SessionStart` and `Stop`.
- Commands live under `[[hooks.<Event>.hooks]]` and use `type = "command"`.
- `SessionStart` emits
  `hookSpecificOutput.additionalContext` with `hookEventName = "SessionStart"`.
- `Stop` exits successfully with valid JSON on stdout.
- memsync writes only user-scope configuration. Project-local hook trust and
  managed enterprise policy are separate Codex controls.

The current hook behavior is documented in the
[official Codex hooks guide](https://developers.openai.com/codex/hooks).
