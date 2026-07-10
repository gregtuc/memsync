# How memsync works

memsync reads the local memory that Claude Code and Codex already generate. It
encrypts selected records into a Git-backed vault and makes relevant records
available to the other tool as read-only reference context. It never edits the
tools' own memory, `CLAUDE.md`, or `AGENTS.md` files.

## Session lifecycle

- Claude Code's `SessionEnd` and Codex's `Stop` hooks capture readable local
  memory. `memsync sync` runs the same capture immediately.
- At `SessionStart`, memsync briefly refreshes the vault and injects relevant
  records. Changes appear in a new session, not live in one that is already
  running.
- Injected context is size-capped and labeled as potentially stale reference
  material. memsync can make memory available; it cannot force a model to use
  or retain it.

## Recall

At session start each tool receives a compact index (one line per memory) of the
other tool's records. To read a record in full, the agent uses memsync's MCP
recall tools:

- `memory_search` finds records by keyword.
- `memory_get` returns a record's full text.

memsync registers this MCP server with each tool during setup and removes it on
uninstall. Recall is read-only. It reaches the memories saved by your other
tools and machines and never exposes or edits the calling tool's own memory.

## Scope and devices

Every installation has a random device ID. memsync hides a tool's own local
records from that same tool while allowing Claude-to-Claude and Codex-to-Codex
delivery from a different device.

Claude memory is project-scoped. Repositories with the same normalized Git
`origin` match across checkout paths and laptops. A project without a Git remote
gets a local-only identity, so its project memory does not automatically match
another checkout.

Codex's generated memory summary is global. Codex creates it asynchronously, so
a new correction may take several sessions to become available.

## Offline behavior

Hooks make a bounded, noninteractive Git attempt and then fail open using the
last encrypted local cache. Local changes remain queued; reconnect and run
`memsync sync`, or let a later hook retry automatically.

## Limits

- memsync supports local Claude Code and Codex CLI memory. It does not bridge
  ChatGPT web memory, Claude web chats, or cloud-only agent sessions.
- Memory is generated and summarized by the tools. It may be delayed,
  incomplete, rewritten, or absent, and its on-disk format can change with a
  tool release.
- A private remote still exposes Git metadata such as update timing and
  encrypted record counts. Record contents remain encrypted.
- Losing the key on every paired laptop makes the vault unrecoverable. Keep a
  protected backup if the memories matter to you.

See [Security](../SECURITY.md) for the threat model and
[Troubleshooting](troubleshooting.md) for repair steps.
