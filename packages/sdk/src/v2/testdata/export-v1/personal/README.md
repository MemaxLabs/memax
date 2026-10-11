# Personal

The Memax record of the personal space `personal`, as of 2026-10-06 10:00 UTC: 2 memories, 0 tombstones, 1 Brief version and 12 receipts.

Every memory is a Markdown file in `memories/`: its statement is the body, and its record
(state, section, trust, sources with their `file:line`, versions, decision fields,
conditions, links and the receipts that touched it) is the YAML frontmatter. A forgotten
memory leaves only a tombstone in `tombstones/`, without its words.

## Files

- `memories/`: one file per memory
- `tombstones/`: one file per forgotten memory, and one per Forget of the whole space
- `brief/`: every version of the Brief as it reads; `current: true` marks the one in force
- `decisions.md`: every decision by where it stands, and the decision gates
- `gates.json`, `targets.json`, `agents.json`, `reads.json`: decision gates, compile targets with their latest compile, agents, and read counts
- `receipts.jsonl`: every receipt in chain order, one per line, with the fields its hash covers
- `checkpoints.json`: the signed checkpoints of the receipt chain, and the keys they are signed with
- `export.json`: the format, the space, and the SHA-256 of every other file

## Verify it

    memax verify-export <this folder>

recomputes the receipt chain from the first receipt, checks it against every signed
checkpoint, checks every file against `export.json`, and reads the memories back against
their receipts.

Receipts 1 to 9 are sealed in 1 checkpoint. 3 receipts came after the last seal: they chain on, but no checkpoint covers them yet.

The Brief in force is [B-0001](brief/B-0001.md).

## Memories

### Preferences (2)

- [M-0001](memories/M-0001.md) Answer in British English, in short paragraphs. · kept
- [M-0002](memories/M-0002.md) Search with rg, not grep. · kept
