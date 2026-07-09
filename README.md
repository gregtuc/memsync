# memsync

**Sync Claude Code and Codex memories — across tools and machines.**
One static binary, one command, and only ciphertext ever touches git.

> 🚧 **Early scaffold.** The core pipeline works today: tool detection, hook
> wiring, an encrypted vault with fail-closed guards, and cross-tool context
> injection. Public-key pairing, `self update`, and the published install
> channels are on the [roadmap](#roadmap). Until then, [build from
> source](#build-from-source).

---

## What it does

Claude Code and Codex each keep their **own** memory — notes the agents write for
themselves. memsync is a **courier, never an editor**: it reads each tool's
memory folder, and shows the *other* tool's memories as read-only context at the
start of each session. Your memory files are never rewritten.

The only thing that ever leaves your machine is an **encrypted git vault** whose
working tree contains nothing but AES-256 ciphertext.

```
reads    each tool's own memory folder          (never edits them — courier, never editor)
injects  the other tool's memories as read-only context at session start (writes no files)
syncs    only AES-256 ciphertext through a private git vault
```

## Install

> Not yet published to Homebrew / the install endpoint. For now, [build from
> source](#build-from-source). The intended experience:

```sh
brew install memsync/tap/memsync      # hero (coming soon)
# or:  curl -fsSL https://memsync.dev/install.sh | sh
```

## Build from source

```sh
git clone https://github.com/gregtuc/memsync
cd memsync
go build -o memsync .
./memsync init
```

## Quickstart — single machine, no remote

```sh
memsync init
```

Detects Claude and/or Codex, wires user-scope hooks, creates your key + local
vault, and runs a round-trip self-test. No account, no remote, no config.
**Restart any open Claude/Codex sessions** to load the hooks. Done.

## How it works

memsync installs small hooks in each tool's user-scope config:

- **SessionStart** → inject the other tool's memories as read-only context.
- **FileChanged / SessionEnd / Stop** → capture new memories into the encrypted vault.

Cross-machine sync is just `git pull`/`push` on the vault. It stays out of the
agents' way: it never edits `MEMORY.md`, never writes Codex's generated files,
and never touches your hand-written `CLAUDE.md` / `AGENTS.md`.

## Security

- **AES-256** envelope; one key at `~/.config/memsync/key` (`0600`) that never
  leaves your machine.
- **Fail-closed guards** (`pre-commit`, `pre-merge-commit`, `pre-push`): nothing
  but valid memsync ciphertext can enter a committed or pushed git object.
- **Local files stay plaintext by design** — the encryption boundary is git.
- No telemetry, no account. See [SECURITY.md](SECURITY.md).

## Sync a second machine

```sh
# machine 1
memsync remote create      # private repo via gh (or: memsync remote set <url>)
memsync pair               # (roadmap) seals the key to the new machine's public key
# machine 2
memsync join               # (roadmap) nothing you copy is a secret
```

## Trust & undo

```sh
memsync doctor    # per-tool table + live self-test
memsync status    # what's synced right now
memsync uninstall # removes only memsync's hooks; --purge also clears key + vault
```

## Roadmap

- [ ] Public-key sealed pairing (`pair` / `join`) — no secret ever copied
- [ ] Swap the AEAD to AES-256-GCM-SIV (nonce-misuse resistant)
- [ ] Near-duplicate detection (SimHash) so paraphrased round-trips don't pile up
- [ ] `self update` with GitHub attestation verification
- [ ] Homebrew tap + `install.sh` + signed/notarized releases + SLSA provenance
- [ ] Optional durable delivery into Codex via `extensions/<name>/notes`

## Honest boundaries

- Sync is a **session-boundary** event, not live into a running session.
- Round-trips are **semantic, not verbatim** — both tools paraphrase memory.
- memsync makes each tool *know* the other's memories; it can't make them *obey*.

## License

MIT — see [LICENSE](LICENSE).
