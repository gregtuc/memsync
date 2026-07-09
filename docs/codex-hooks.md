# Codex hook wiring

memsync wires itself into Codex via a marker-delimited block in the user-scope
`~/.codex/config.toml` (`internal/hooks/codex.go`). The schema below was verified
against the current Codex docs (developers.openai.com/codex, which redirect to
learn.chatgpt.com/docs).

```toml
# >>> memsync (managed) - do not edit this block >>>
[features]
hooks = true

[[hooks.SessionStart]]

[[hooks.SessionStart.hooks]]
type = "command"
command = "/abs/path/memsync inject --tool codex"

[[hooks.Stop]]

[[hooks.Stop.hooks]]
type = "command"
command = "/abs/path/memsync sync --tool codex"
# <<< memsync (managed) <<<
```

Schema rules that matter:

- Event names are PascalCase: `SessionStart`, `Stop`. Lowercase `session_start`
  or `stop` are not recognized and never fire.
- A command is a single string nested under `[[hooks.<Event>.hooks]]` with
  `type = "command"`. It is not an array, and it does not sit directly on the
  `[[hooks.<Event>]]` table.
- Hooks are enabled by default; `[features] hooks = true` is the canonical key
  (`codex_hooks` is a deprecated alias). Keeping it explicit is harmless.
- A `SessionStart` hook injects context by printing either plain text or
  `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"..."}}`
  on stdout. memsync prints the JSON form (`cmd/inject.go`).
- A `Stop` hook must print valid JSON on stdout when it exits 0. memsync's
  `sync` prints `{}` (`cmd/sync.go`).

Notes:

- memsync only writes user-scope config. Repo-local `.codex/config.toml` hooks
  have not fired reliably in interactive sessions (openai/codex#17532).
- Under `--strict-config` an unknown or mistyped key refuses to start, so the
  block must stay schema-valid. It is removed verbatim by `memsync uninstall`,
  and `memsync init --dry-run` previews it.
