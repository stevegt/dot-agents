# dot-agents

`dot-agents` is a public collection of generic agent instructions, skills, and
the programs owned by those skills. The repository root mirrors the contents of
an `.agents` directory; it does not contain another `.agents/` directory and it
does not install or synchronize live agent configuration.

## Skills

- [`todo-stack`](skills/todo-stack/SKILL.md) manages five readable Markdown
  task stacks with deterministic ordering, atomic moves, concurrent-process
  locking, crash recovery, validation, and Graphviz output.
- [`commit`](skills/commit/SKILL.md) creates a self-reviewed Git commit whose
  message explains changed content in plain English.
- [`replan`](skills/replan/SKILL.md) reviews and restates a proposed plan as a
  complete replacement.
- [`tutor`](skills/tutor/SKILL.md) presents long technical material in small,
  assessed lessons.

Skills use uppercase `SKILL.md` for current Codex compatibility.

## Install todo-stack

```sh
go install github.com/stevegt/dot-agents/skills/todo-stack/scripts/todo-stack@latest
```

Go 1.25 or newer is required.

## Use todo-stack

```sh
todo-stack init
todo-stack push --input task.md
todo-stack peek
todo-stack move --from default --to completed --id 123456789
todo-stack validate
```

Commands use `stacks/default/` unless `--root` or `--scope` selects another
location. Each scope contains `inbox.md`, `default.md`, `blocked.md`,
`completed.md`, and `wontfix.md`; file tail is stack top.

Use `--expect <stack>=<CID>` and `--expect-item <CID>` to reject a write when
state changed after it was read. Moving work to `wontfix.md` and clearing a
stack require `--approval` evidence so a caller cannot discard work silently.
See the [todo-stack specification](skills/todo-stack/references/todo-stack.md)
for the item schema, command behavior, and recovery model.

## Development

```sh
make all
```

Production compilation is checked with `CGO_ENABLED=0`.

## License

Copyright (C) 2026 Steve Traugott. This project is licensed under the GNU
General Public License, version 3 or any later version at your option. See
[`LICENSE`](LICENSE). SPDX-License-Identifier: GPL-3.0-or-later.
