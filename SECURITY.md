# Security

memsync has one job at the security layer: nothing but ciphertext ever leaves
your machine. Local plaintext is intentional and fine, because your agent tools
already keep tokens and notes in the clear locally. The boundary memsync enforces
is git.

## What is encrypted, and where

| Data | On disk locally | In the git vault or on a remote |
|------|-----------------|--------------------------------|
| Your agents' own memory files | plaintext (untouched by memsync) | never stored |
| memsync's captured and couriered records | plaintext mirror (never committed) | AES-256 ciphertext only |
| The encryption key | `~/.config/memsync/key`, dir `0700`, file `0600` | never leaves the machine |

## The guarantee

The vault's working tree contains only memsync envelopes. Guards run on every
`pre-commit`, `pre-merge-commit`, and `pre-push`, and assert a positive
invariant: every file must carry the memsync magic header and decrypt under the
local key. Anything else (plaintext, corrupt, or foreign) aborts the git
operation. Because plaintext is never written into that tree, packfiles, reflog,
stash, and gc have no plaintext object to preserve.

Guards are installed via `core.hooksPath`, not `.git/hooks` (which is not cloned),
and are re-provisioned at every bootstrap. `sync` fails closed if guards are
absent.

## Envelope and cipher

Layout: `magic(8) || nonce(12) || AES-256-GCM(ciphertext||tag)`, keyed with a
256-bit key from the OS CSPRNG.

The nonce is not random. It is derived from the content:
`nonce = HMAC-SHA256(key, plaintext)`. Two consequences:

1. A nonce is never reused across two different messages, which is the exact
   condition AES-256-GCM needs to stay secure. There is no caller-controlled
   nonce to misuse.
2. Identical content produces identical ciphertext, so re-syncing an unchanged
   record is a git no-op.

The accepted cost of (2) is that this is deterministic encryption: someone who
steals the vault can tell whether two records are identical. Record file names
are content-addressed on identity, not on plaintext, so names do not reveal
content. An optional switch to RFC 8452 AES-256-GCM-SIV (via a maintained
library) is on the roadmap; the current construction already prevents nonce
reuse across distinct messages.

## Pairing

Adding a second machine uses X25519 public-key sealing (stdlib `crypto/ecdh`).
The joining machine shares a public key. The existing machine seals the vault key
to that public key. Only the joining machine's private key, which never leaves it,
can open the reply. Nothing copied between machines is a secret.

## Known limits (honest)

- If you enable optional durable delivery into Codex's `extensions/notes`, Codex
  commits those plaintext notes into its own local git repo
  (`~/.codex/memories/.git`). That repo has no remote and memsync never gives it
  one, so it is accepted local plaintext that never crosses a machine.
- Deterministic encryption leaks equality (whether two records are identical) to
  anyone who obtains the vault.

## Reporting a vulnerability

Open a private security advisory on the GitHub repository, or email the
maintainer. Please do not file public issues for security reports.
