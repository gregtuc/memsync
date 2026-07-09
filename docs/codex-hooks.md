# Codex hook wiring — verification note

memsync wires itself into Codex via a marker-delimited block in
`~/.codex/config.toml` (`internal/hooks/codex.go`):

```toml
# >>> memsync (managed) — do not edit this block >>>
[features]
hooks = true

[[hooks.session_start]]
command = ["/abs/path/memsync", "inject", "--tool", "codex"]

[[hooks.stop]]
command = ["/abs/path/memsync", "sync", "--tool", "codex"]
# <<< memsync (managed) <<<
```

Codex's hook schema is shipped but lightly documented and has moved between
versions. Before trusting the block above, confirm against your Codex version:

- the feature flag key (`[features] hooks`) and whether it defaults on;
- the canonical hook table name (`hooks` vs the legacy `codex_hooks` alias);
- the event names (`session_start`, `stop`) and the command array shape;
- the SessionStart output schema memsync emits in `cmd/inject.go`
  (`hookSpecificOutput.additionalContext`).

Known gotcha: repo-local `config.toml` hooks may not fire
(openai/codex#17532) — memsync only ever writes **user-scope** config.

The block is always valid TOML and is removed verbatim by `memsync uninstall`,
so a wrong key is inert, not destructive. `memsync init --dry-run` previews it.
