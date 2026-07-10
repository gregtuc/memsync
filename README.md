<p align="center">
  <img src="assets/logo.svg" width="120" alt="memsync"/>
</p>

<h1 align="center">memsync</h1>

<p align="center">
  <b>One shared memory for Claude Code and Codex.</b><br/>
  Automatic on one laptop or across several. Local-first, encrypted, and private.
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#another-laptop">Another laptop</a> ·
  <a href="#how-memory-moves">How it works</a> ·
  <a href="SECURITY.md">Security</a>
</p>

---

Claude Code and Codex remember different things. memsync keeps them in sync so
either tool can pick up what the other learned.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/gregtuc/memsync/main/install.sh | sh
```

That's it. Restart Claude Code and Codex. When Codex asks, approve memsync.

## Another laptop

On the new laptop, install memsync. Then open a new terminal and run:

```sh
memsync join
```

Then, on the laptop that already has memsync:

```sh
memsync pair
```

Follow the prompts. memsync creates the private sync repository and verifies the
connection for you. If GitHub needs a sign-in, it tells you exactly what to do.

## How memory moves

- It quietly captures useful context after a coding session and makes relevant
  context available when the next session begins.
- Each tool sees a short index of the other tool's memories at the start of a
  session, and can read any of them in full on demand through memsync's
  `memory_search` and `memory_get` tools.
- After installation, one laptop needs no account or network. Across laptops,
  memsync uses a private encrypted sync repository.
- It never edits either tool's own memory, `CLAUDE.md`, or `AGENTS.md` files.
- If the network is unavailable, sessions keep working and sync catches up later.

See [how memsync works](docs/how-it-works.md) for the technical details.

## Security

- Memory is encrypted before it leaves your laptop. The key stays only on the
  laptops you connect.
- memsync refuses to sync plaintext, corrupted data, or data encrypted with the
  wrong key.
- There is no memsync account, service, or telemetry.

See [SECURITY.md](SECURITY.md) for the threat model, metadata limits, pairing
authenticity, and key-recovery guidance.

## Commands

| Command | Purpose |
|---|---|
| `memsync init` | Set up or refresh this laptop |
| `memsync status` | Show current setup |
| `memsync sync` | Sync now |
| `memsync doctor` | Find or fix setup problems |
| `memsync join` | Connect this laptop to another |
| `memsync pair` | Add another laptop |
| `memsync uninstall` | Disconnect memsync from the tools |

Run `memsync --help` for advanced remote options. See the
[short repair guide](docs/troubleshooting.md) if anything needs attention.

## License

MIT. See [LICENSE](LICENSE).
