# Troubleshooting

Start with these two commands:

```sh
memsync status
memsync doctor
```

`status` is the quick picture. `doctor` validates tool features, hook settings,
observed hook activity, key permissions, ciphertext integrity, and remote access.
If it finds a repairable local problem, run:

```sh
memsync doctor --fix
memsync doctor
```

## Codex says “configured; not observed”

Open Codex, enter `/hooks`, and trust both memsync hooks. Then start a fresh
session and check `memsync status` again. If the memsync binary moved since
setup, run `memsync doctor --fix` first and review the refreshed definitions.

## Codex contributes zero memories

Codex Memories is off by default and generated asynchronously. Enable it with:

```sh
memsync init --enable-codex-memories
```

Do a few eligible Codex tasks, allow them to become idle, and check again later.
The feature may use a small amount of Codex quota. Claude → Codex delivery can
still work while Codex Memories is off.

## Claude says hooks are disabled

If `memsync doctor` reports `disableAllHooks=true`, remove that user-wide setting
from `~/.claude/settings.json` or set it to `false`, then restart Claude Code.
memsync does not override a setting that intentionally disables every Claude
hook on the machine.

## Claude memory from another checkout is absent

Claude memory is project-scoped. Across laptops, memsync matches repositories by
their normalized Git `origin`, not by local folder name. Confirm both checkouts
refer to the same remote:

```sh
git remote get-url origin
```

A repository without an `origin` gets a local-only project identity. Run
`memsync sync` from inside the intended repository after its native Claude memory
exists.

## The private remote cannot be reached

Pairing transfers the vault key, not Git credentials. Authenticate on every
laptop that uses the remote:

```sh
gh auth login
gh auth setup-git
```

For a non-GitHub remote, configure its SSH key or credential helper directly.
Do not put a username, password, or token inside an HTTP(S) remote URL; memsync
rejects credential-bearing URLs to keep them out of pairing and diagnostics.
Then retry with `memsync sync`. Network operations are bounded and
noninteractive, so a hook uses cached context instead of waiting indefinitely.

## A laptop was offline

This is safe. Capture and injection use the local encrypted vault. Other laptops
remain stale until a push succeeds. Reconnect and run:

```sh
memsync sync
```

Later session hooks also retry automatically.

## The encryption key is missing

If a vault or remote already exists, memsync refuses to generate a replacement
key because it would be unable to decrypt the existing records. Restore
`~/.config/memsync/key` from a paired laptop or protected backup and set it to
mode `0600`. To rebuild this laptop instead, run `memsync uninstall --purge`,
then `memsync init` and pair it again from a healthy laptop. Do not delete the
last healthy key copy.

## Setup changed a tool configuration

memsync writes only its user-scope hook entries and creates a backup beside the
tool configuration before editing it. `memsync uninstall` removes the managed
entries without deleting the encrypted vault or key. Use
`memsync uninstall --purge` only when you also want to delete local memsync
state; it does not delete the remote repository.

When asking for help, do not post native memory files, the memsync key, pairing
tokens, or remote URLs containing credentials.
