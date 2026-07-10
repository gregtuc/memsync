<p align="center">
  <img src="assets/logo.svg" width="120" alt="memsync"/>
</p>

<h1 align="center">memsync</h1>

<p align="center">
  <b>Share useful local memory between Claude Code and Codex, on one laptop or several.</b><br/>
  Agent files stay local. Records stored in Git are encrypted with AES-256-GCM.
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#set-up-one-laptop">One laptop</a> ·
  <a href="#add-another-laptop">More laptops</a> ·
  <a href="#how-memory-moves">How it works</a> ·
  <a href="SECURITY.md">Security</a>
</p>

---

Claude Code and Codex build separate local memories as they work. memsync reads
those generated memory files, encrypts selected records into a local vault, and
adds relevant records to the next session as reference context. It never edits
the tools' own memory, `CLAUDE.md`, or `AGENTS.md` files.

Start with one laptop—no account or network required. If you later want the
same memory on another laptop, connect a private Git remote and pair it once.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/gregtuc/memsync/main/install.sh | sh
```

## Set up one laptop

Install memsync, then run:

```sh
memsync init
```

`init` detects the local tools, creates an encryption key and device identity,
installs user-level hooks, and captures the readable memories for the current
project.

If Codex is present, setup asks whether to enable **Codex Memories**. This is
required for Codex → Claude or Codex → another laptop. It is optional for
Claude → Codex, is off by default in Codex, generates summaries in the
background, and may use a small amount of Codex quota. For unattended setup,
choose explicitly:

```sh
memsync init --enable-codex-memories
# or leave it off:
memsync init --no-codex-memories
```

Codex also requires one manual security review before new command hooks can run:

1. Open a new Codex session.
2. Choose **Review hooks**, or enter `/hooks`.
3. Review and trust both memsync hooks.
4. Restart any Claude Code or Codex sessions that were already open.

Then check the result:

```sh
memsync status
memsync doctor
```

`status` shows memory counts, encrypted records, and whether each hook has ever
been observed. `doctor` performs deeper checks; `memsync doctor --fix` repairs
hook wiring, the local vault, and other safe setup state.

## Add another laptop

First install memsync and run `memsync init` on both laptops. Across machines,
Git transports the encrypted vault. Both laptops therefore need Git access to
the same private repository; the pairing token provides the encryption key but
does **not** grant repository access.

For the built-in GitHub flow, install the `gh` CLI and authenticate on both
laptops:

```sh
gh auth login
gh auth setup-git
```

Now follow this order:

1. On the **existing laptop**, create the private vault repository:

   ```sh
   memsync remote create
   ```

   To use an existing private Git repository instead:

   ```sh
   memsync remote set git@github.com:you/memsync-vault.git
   ```

2. On the **new laptop**, start pairing and leave the command open:

   ```sh
   memsync join
   ```

   Copy the invite it prints and keep its short verification code visible.

3. Back on the **existing laptop**:

   ```sh
   memsync pair
   ```

   Paste the invite. memsync shows the same verification code and asks you to
   confirm it. Compare the two laptop screens (or use a separate authenticated
   channel), confirm only when they match, then copy the sealed reply.

4. Paste the reply into the waiting `memsync join` command on the new laptop.
   Restart the tools there, complete Codex's `/hooks` review if applicable, and
   run `memsync status`.

The invite is a public key, not a password, but it still must be authentic. The
verification-code comparison detects a substituted invite; do not approve a
mismatch. The sealed reply contains no plaintext key.

## How memory moves

- Claude Code's `SessionEnd` and Codex's `Stop` hooks capture readable local
  memory. `memsync sync` runs the same capture immediately when you want it.
- At `SessionStart`, memsync briefly refreshes the Git vault and injects relevant
  records as read-only context. Changes appear in a new session, not live in one
  that is already running.
- Each installation has a random device ID. memsync hides a tool's own local
  records from that same tool, while allowing Claude-to-Claude and Codex-to-Codex
  delivery from a different device.
- Claude memories are project-scoped. Repositories with the same normalized Git
  `origin` match across different checkout paths and laptops. A project without
  a Git remote receives a local-only identity, so its project memory does not
  automatically match another checkout.
- Codex's generated memory summary is global. Codex creates it asynchronously,
  so a new correction may take several sessions to become available to memsync.
- When offline, hooks make a bounded, noninteractive Git attempt, then fail open
  and use the last encrypted local cache. Session startup can pause briefly for
  that attempt. Local changes remain queued in Git; reconnect and run
  `memsync sync`, or let a later hook retry the pull and push.

Injected context is size-capped and labeled as potentially stale reference
material. memsync makes a memory available; it cannot force a model to use or
retain it.

## Security

- Vault records use AES-256-GCM with a fresh random nonce for every encryption.
  Unchanged plaintext is detected before encryption so routine syncs do not
  rewrite records unnecessarily.
- The key is stored with private permissions on every paired laptop. Pairing
  sends it only inside an X25519-sealed reply; it is never committed to Git.
- Git guards reject plaintext, corrupt records, and records encrypted under the
  wrong key before memsync commits or pushes them.
- There is no memsync account, service, or telemetry.

See [SECURITY.md](SECURITY.md) for the threat model, metadata limits, pairing
authenticity, and key-recovery guidance.

## Commands

| Command | Purpose |
|---|---|
| `memsync init` | Set up or refresh this laptop |
| `memsync status` | Show sources, vault records, mode, and observed hooks |
| `memsync sync` | Capture local memories and retry remote sync now |
| `memsync doctor` | Diagnose setup (`--fix` repairs safe local state) |
| `memsync remote create` | Create or reuse a private GitHub vault repository |
| `memsync remote set <url>` | Use another private Git repository |
| `memsync join` | New laptop: create an invite and accept the sealed reply |
| `memsync pair` | Existing laptop: seal its vault access for an invite |
| `memsync uninstall` | Remove memsync hooks (`--purge` also removes local state) |

## Expectations and limitations

- memsync supports local Claude Code and Codex CLI memory. It does not bridge
  ChatGPT web memory, Claude web chats, or cloud-only agent sessions.
- Memory is generated and summarized by the tools themselves. It may be delayed,
  incomplete, rewritten, or absent, and its on-disk format can change with a tool
  release. Run `memsync doctor` after upgrading Claude Code or Codex.
- Claude project matching across laptops requires the checkouts to share a Git
  remote identity. Global Codex memory is not project-filtered.
- A private remote still exposes Git metadata such as update timing and encrypted
  record counts. Record contents remain encrypted.
- Losing the key on every paired laptop makes the vault unrecoverable. Keep an
  appropriately protected backup if the memories matter to you.

See [docs/troubleshooting.md](docs/troubleshooting.md) for the short repair guide.

## License

MIT. See [LICENSE](LICENSE).
