# Todo Stack

This specification defines a file-backed task-ordering mechanism. Five tracked
Markdown files hold visible task state. Ignored runtime files serialize writes
and recover interrupted mutations. The command performs no Git, network, or
process-management operation.

## Stack files

Each scope owns these files below `stacks/<scope>/`:

| File | Meaning |
| --- | --- |
| `inbox.md` | Received work not yet classified |
| `default.md` | Runnable work |
| `blocked.md` | Work waiting for a named event |
| `completed.md` | Work whose acceptance checks passed |
| `wontfix.md` | Work directly approved for permanent abandonment |

File tail is stack top. File position supplies processing order. IDs identify
items but do not supply time, priority, or authority.

## Item format

Each item has this form:

```markdown
# 1792354830123456789 — Add crash recovery

## Actor
storage-maintainer

## Purpose
Keep an interrupted two-file move recoverable.

## Requested result
Recover a recorded move before any later command.

## Acceptance
- A crash after every journal phase reaches the selected final state.

## Graph
- Name: crash_recovery
- Kind: task
- URL: todo-stack.md#atomicity-and-recovery

## Prerequisites
None.

## Source evidence
- issue 42

## Details
Free-form Markdown without another level-1 heading.
```

Graph kinds are `task`, `decision`, and `terminal`. Graph names are unique DOT
identifiers. Local URLs resolve relative to `specs/` and may not escape the
repository. A prerequisite record has four `|`-separated fields:

```text
<scope>/<ID> | desired | edge label | todo-stack.md#item-format
```

`ordinary` is the other edge class. A local prerequisite may use a bare ID.
Canonical output uses `<scope>/<ID>`.

New input uses `# new — <title>`. Within one batch, `alias:<graph-name>` may
refer to another new item or an existing item. The command replaces aliases
with canonical references and records the import alias in Source evidence.

## Command interface

`todo-stack` accepts flags before or after its command:

- `--root` defaults to `stacks`.
- `--scope` defaults to `default`.
- `--stack` defaults to `default` for one-stack commands.
- `--input` and `--output` replace standard input and output.
- Repeatable `--expect <stack>=<CID>` and `--expect-item <CID>` reject stale
  decisions.

Commands are:

- `init` creates missing standard files without replacing existing files.
- `push` appends one or more `new` items; the last input item becomes top.
- `pop` emits and removes top, or `--id` selects another item.
- `peek` emits top without removal; `--depth` or `--id` selects another item.
- `empty` prints `true` or `false`.
- `list` emits canonical items from file beginning to file tail.
- `clear` requires expected stack revision, reason, and approval evidence.
- `move --from <stack> --to <stack>` moves top unless `--id` selects another
  item.
- `validate` checks all five files, references, cycles, states, and URLs.
- `graph` generates a prerequisite graph for the selected scope.

`push` has no `--id` form. A same-stack move without placement promotes the
selected item to top. `move --after-id <anchor-ID>` places the selected item
immediately after the anchor in processing order, which stores it immediately
before the anchor in file order.

Moving work into `blocked.md` needs a reason naming the wait. Restoring it to
`default.md` needs a reason naming verified evidence. Moving work into
`wontfix.md` needs a reason and direct approval evidence. Items never move out
of `wontfix.md`.

Output starts with applicable stack and item revision CIDs. Exit 0 means
success; exit 1 means storage or recovery failure; exit 2 means invalid input;
exit 3 means stale state, refused movement, or another coordination conflict.

`pop` writes and syncs output before recording removal. A crash can repeat
output but cannot silently discard it. Ordinary task handling should use
`peek` and `move`.

## Atomicity and recovery

Ignored runtime state lives below `stacks/.runtime/<scope>/`:

- `lock` serializes processes with an advisory file lock;
- `journal.json` describes the active mutation;
- `images/` contains complete replacement bytes;
- `operations.jsonl` records time, operation ID, command, stack names, item
  IDs, outcome, and error category without item bodies.

Every command acquires the scope lock and completes an existing journal before
reading or writing stacks. A mutation writes and syncs replacement images,
writes and syncs the journal, installs every target with same-directory atomic
rename and directory sync, appends and syncs one operation record, then removes
and syncs the journal and images.

The command confines managed paths below a non-symlink root, rejects symlinks
and path traversal, and never tracks locks or recovery files. A raw reader may
briefly see one side of a two-file move; command clients recover one selected
final state.

## Task transitions

`default.md` holds runnable work. A successful task moves to `completed.md`.
A named external wait moves to `blocked.md` and returns only after its reason is
resolved. Permanent abandonment requires direct approval for the exact item and
reason; the command records that evidence in the item.

## Generated prerequisite graph

Graph generation includes all runnable and blocked items, their prerequisite
closure, and the two newest completed items. It excludes unclassified inbox
items and rejects runnable work that depends on a wontfix item.

Every item becomes one actor-and-purpose node. Completed nodes are green,
wontfix terminals are red, successful terminals are double-outline ellipses,
and desired-path edges are heavier. Added or semantically changed nodes and
edges receive bright-green review strokes. A `--base` graph preserves prior
markers when meaning is unchanged; repeated generation is byte-stable.

Graph generation is available for every scope. A consuming repository decides
whether generated graphs are authoritative, review aids, or unused.
