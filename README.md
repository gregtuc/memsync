<p align="center">
  <img src="assets/logo.svg" width="120" alt="memsync"/>
</p>

<h1 align="center">memsync</h1>

<p align="center">
  <b>One shared memory for Claude Code and Codex — across both tools and all your machines.</b><br/>
  Local stays plaintext. Anything that leaves your machine is AES-256 encrypted.
</p>

<p align="center">
  <a href="#quickstart">Quickstart</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#security">Security</a> ·
  <a href="SECURITY.md">Threat model</a>
</p>

---

Claude Code and Codex each keep their own memory — the notes they write for
themselves as they learn how you work. The problem: that memory is trapped.
What you teach Claude is invisible to Codex, and none of it follows you to your
other laptop.

memsync fixes that. Teach one agent something once, and the other knows it too —
on every machine you use.

```text
you correct Claude on your desktop  →  Codex on your laptop already knows
Codex figures something out         →  Claude sees it next session
```

## Quickstart

One machine, no account, nothing to set up:

```sh
go install github.com/gregtuc/memsync@latest    # or: git clone … && go build -o memsync .
memsync init
```

`init` finds Claude and/or Codex, installs a few small hooks, creates your key
and a local encrypted vault, and runs a self-test. Restart any open Claude/Codex
sessions and you're done — their memories are now shared locally.

## Use it on more than one machine

You don't create anything on GitHub yourself — memsync does it:

```sh
# on the machine you already use
memsync remote create     # makes a PRIVATE repo for you (via the gh CLI)
memsync pair              # paste in the new machine's invite, get a sealed reply

# on the new machine
memsync join             # prints an invite, then takes the sealed reply — done
```

Nothing you copy during pairing is a secret: the invite is a public key, and the
reply is sealed so only the new machine can open it. Prefer to bring your own
repo? `memsync remote set git@github.com:you/your-vault.git`. **Only use one
machine? Skip this whole section** — no account, no repo, nothing.

## How it works

memsync installs small hooks in each tool's own user-scope config. After that:

- **an agent writes a memory** → memsync captures it and encrypts it into a private git vault
- **a session starts** → memsync shows that session the *other* tool's memories as read-only context

memsync only reads your agents' memory folders — it doesn't write to them, relocate
them, or touch your `CLAUDE.md` / `AGENTS.md`. Nothing about how Claude or Codex
behave changes; they just gain awareness of each other.

Across machines, the encrypted vault is an ordinary private git repo. That's the
only thing that ever travels — `git pull`/`push` under the hood, no service in the
middle.

## Security

- **AES-256** envelope. One key at `~/.config/memsync/key` (`0600`) that never leaves your machine.
- The vault's working tree holds **only ciphertext**. Fail-closed `pre-commit` /
  `pre-merge-commit` / `pre-push` guards reject anything that isn't valid memsync
  ciphertext — so plaintext can't reach a committed or pushed git object.
- **Local files stay plaintext on purpose.** Your agents already keep tokens and
  notes in the clear locally; the boundary memsync enforces is git.
- Pairing uses X25519 public-key sealing — the key is never sent in the clear.
- No telemetry, no account, zero third-party dependencies. Details in [SECURITY.md](SECURITY.md).

## Commands

```text
memsync init        set everything up (idempotent)
memsync doctor      status table + live self-test  (--fix repairs)
memsync status      what's synced right now
memsync remote      create a private vault repo (or set your own)
memsync pair/join   add another machine (public-key sealed)
memsync uninstall   remove memsync's hooks  (--purge also clears key + vault)
```

## Status & roadmap

The core works today: detection, hooks, the encrypted vault with guards,
cross-tool injection, and multi-machine pairing + sync.

- [x] Public-key sealed pairing (`pair` / `join`)
- [x] Vault-backed cross-machine sync
- [ ] AES-256-GCM-SIV (nonce-misuse-resistant AEAD)
- [ ] Near-duplicate detection so paraphrased round-trips don't pile up
- [ ] `self update` with signed-release verification
- [ ] Homebrew tap + one-line installer + signed, notarized releases

## What to expect

- Memories show up **at the next session**, not live inside a running one.
- Both tools rewrite memory in their own words, so a shared fact is kept **once
  per side**, not as identical text.
- memsync makes each tool *know* what the other learned — it doesn't force either
  to act on it.

## License

MIT — see [LICENSE](LICENSE).
