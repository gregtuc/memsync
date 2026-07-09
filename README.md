<p align="center">
  <img src="assets/logo.svg" width="120" alt="memsync"/>
</p>

<h1 align="center">memsync</h1>

<p align="center">
  <b>One shared memory for Claude Code and Codex, across both tools and all your machines.</b><br/>
  Local files stay plaintext. Anything that leaves your machine is AES-256 encrypted.
</p>

<p align="center">
  <a href="#quickstart">Quickstart</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#security">Security</a> ·
  <a href="SECURITY.md">Threat model</a>
</p>

---

Claude Code and Codex each keep their own memory, the notes they write for
themselves as they learn how you work. The trouble is that memory is trapped.
What you teach Claude is invisible to Codex, and none of it follows you to your
other laptop.

memsync fixes that. Teach one agent something once and the other knows it too,
on every machine you use.

```text
you correct Claude on your desktop   ->  Codex on your laptop already knows
Codex figures something out          ->  Claude sees it next session
```

## Quickstart

One machine, no account, nothing to set up:

```sh
go install github.com/gregtuc/memsync@latest    # or: git clone … && go build -o memsync .
memsync init
```

`init` finds Claude and Codex, installs a few small hooks, creates your key and a
local encrypted vault, and runs a self-test. Restart any open Claude or Codex
sessions and their memories are shared locally.

## Use it on more than one machine

You do not create anything on GitHub yourself. memsync does it for you:

```sh
# on the machine you already use
memsync remote create     # makes a PRIVATE repo for you (via the gh CLI)
memsync pair              # paste in the new machine's invite, get a sealed reply

# on the new machine
memsync join             # prints an invite, then takes the sealed reply
```

Nothing you copy during pairing is a secret. The invite is a public key, and the
reply is sealed so only the new machine can open it. Want to use your own repo
instead? Run `memsync remote set git@github.com:you/your-vault.git`. Only use one
machine? Skip this whole section. No account, no repo, nothing.

## How it works

memsync installs small hooks in each tool's own user-scope config. After that:

- when an agent writes a memory, memsync captures it and encrypts it into a private git vault
- when a session starts, memsync shows that session the other tool's memories as read-only context

memsync only reads your agents' memory folders. It does not write to them, move
them, or touch your `CLAUDE.md` or `AGENTS.md` files. Nothing about how Claude or
Codex behave changes. They just gain awareness of each other.

Across machines, the vault is an ordinary private git repository, and it is the
only thing that ever travels. memsync moves it with plain `git pull` and `push`.
There is no server in the middle.

## Security

- AES-256 encryption. One key at `~/.config/memsync/key` (`0600`) that never leaves your machine.
- Encryption is deterministic: the nonce comes from the content, so a nonce is
  never reused across different records, and identical content stays identical so
  the vault does not churn. The tradeoff is that someone who steals the vault can
  tell whether two records are equal. See [SECURITY.md](SECURITY.md).
- The vault's working tree holds only ciphertext. Fail-closed `pre-commit`,
  `pre-merge-commit`, and `pre-push` hooks reject anything that is not valid
  memsync ciphertext, so plaintext cannot reach a committed or pushed git object.
- Local files stay plaintext on purpose. Your agents already keep tokens and
  notes in the clear locally. The boundary memsync enforces is git.
- Pairing uses X25519 public-key sealing, so the key is never sent in the clear.
- No telemetry, no account.

## Commands

```text
memsync init        set everything up (safe to run again)
memsync doctor      status table plus a live self-test  (--fix repairs)
memsync status      what is synced right now
memsync remote      create a private vault repo, or set your own
memsync pair/join   add another machine (public-key sealed)
memsync uninstall   remove memsync's hooks  (--purge also clears key and vault)
```

## Status and roadmap

The core works today: detection, hooks, the encrypted vault with guards,
cross-tool injection, and multi-machine pairing and sync.

- [x] Public-key sealed pairing (`pair` and `join`)
- [x] Vault-backed cross-machine sync
- [x] Deterministic encryption so the vault does not churn on re-sync
- [x] Near-duplicate detection so reworded echoes do not pile up
- [ ] Homebrew tap and a one-line installer with signed, notarized releases
- [ ] Optional RFC 8452 AES-256-GCM-SIV via a maintained library

## What to expect

- Memories show up at the next session, not live inside a running one.
- Both tools rewrite memory in their own words, so a shared fact is kept once per
  side rather than as identical text.
- memsync makes each tool aware of what the other learned. It does not force
  either tool to act on it.

## License

MIT. See [LICENSE](LICENSE).
