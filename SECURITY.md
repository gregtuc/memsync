# Security

memsync's one job at the security layer: **nothing but ciphertext ever leaves
your machine.** Local plaintext is intentional and fine — your agent tools
already keep tokens and secrets in plaintext locally. The boundary memsync
enforces is git.

## What is encrypted, and where

| Data | On disk locally | In the git vault / on a remote |
|------|-----------------|-------------------------------|
| Your agents' own memory files | plaintext (untouched by memsync) | never stored |
| memsync's captured/couriered records | plaintext mirror (never committed) | **AES-256 ciphertext only** |
| The encryption key | `~/.config/memsync/key`, dir `0700`, file `0600` | **never leaves the machine** |

## The guarantee

The vault's working tree contains only memsync envelopes. Guards run on every
`pre-commit`, `pre-merge-commit`, and `pre-push` and assert a **positive
invariant**: every file must carry the memsync magic header *and* decrypt under
the local key. Anything else — plaintext, corrupt, or foreign — aborts the git
operation. Because plaintext is never written into that tree, packfiles, reflog,
stash, and gc have no plaintext object to preserve.

Guards are installed via `core.hooksPath` (not `.git/hooks`, which is not cloned)
and re-provisioned at every bootstrap. `sync` fails closed if guards are absent.

## Envelope

`magic(8) || nonce(12) || AEAD(ciphertext||tag)`, keyed with a 256-bit key from
the OS CSPRNG. The current scaffold uses AES-256-GCM with a random nonce and
re-encrypt-on-change; the target is AES-256-GCM-SIV (nonce-misuse resistant).
The AEAD is isolated to one function (`internal/crypto`) for a clean swap.

## Known limits (honest)

- If you enable the optional durable delivery into Codex's `extensions/notes`,
  Codex commits those plaintext notes into its **own local** git repo
  (`~/.codex/memories/.git`). That repo has no remote and memsync never gives it
  one — it is accepted local plaintext, never crossing a machine.
- Deterministic-per-content encryption (once GCM-SIV lands) leaks *equality*
  (whether two records are identical) to someone who steals the vault. Filenames
  are content-addressed on identity, not plaintext.

## Reporting a vulnerability

Open a private security advisory on the GitHub repository, or email the
maintainer. Please do not file public issues for security reports.
