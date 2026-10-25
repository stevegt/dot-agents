---
name: commit
description: Create and self-review one Git commit that carries a plain-English message. Use whenever a worker, including main, commits implementation or documentation.
---

# Commit

Do not write commit messages that only describe process, history, or
workflow. The commit message must describe the change in product
behavior, failure behavior, a rule, data meaning, a documentation
requirement, or a test case.  See "Required: Describe Content, Not
Process" below.

Use plain english and avoid jargon unless it is used elsewhere in the
repository.

## Hard Stop: Name Content Before Git Mechanics

This gate applies to ordinary commits, merge commits, and commit objects created
before branch installation because a required check needs an object.
The object-required path is not an exception.
Its subject and opening `Before:` and `After:` sentences first describe what
the files, software, or repository rule does. Mention the temporary object,
tree, parents, tree pinning, or validation purpose only later in `Details:`.

```text
Bad:  Pin the combined demo tree for validation
Good: Keep the demo unable to contact external services

Bad:  The temporary object did not contain the merged result
Good: The command did not include the offline demo
```

Stop and rewrite if any first-three audit claim says only that Git records,
pins, combines, validates, or identifies something. Do not compose or invoke
`git commit` or `git commit-tree` until all three claims say what changed in
behavior, failure behavior, a rule, data meaning, documentation, or a test.

## Prepare

1. Read the governing docs. Confirm that paths,
   behavior, naming, side effects, barriers, and stop conditions are decided.
2. Inspect the complete status and diff. Stage only individually named intended
   files. Preserve unrelated changes and never use broad staging.
3. Run the checks that apply to the exact tree being promised. State checks not
   run and unfinished scope in the commit body.

## Hard Stop: Supply Every Message Through a Quoted Here-Document

Whenever this skill authors or replaces a commit message, pass the complete
message on standard input with `-F -` and a single-quoted here-document
delimiter:

```sh
git commit -F - <<'EOF'
Concrete imperative subject

Before:
...

After:
...

Planned Actions:
...

Intervention Needed:
...

Details:
...
EOF
```

This is mandatory for ordinary commits, amendments, merge commits, and commit
objects made with `git commit-tree`. Use the corresponding forms:

```sh
git commit --amend -F - <<'EOF'
...
EOF

git commit-tree TREE -p PARENT -F - <<'EOF'
...
EOF
```

For a merge that needs a merge commit, run `git merge --no-ff --no-commit`
first and then create the reviewed commit with `git commit -F -` and the quoted
here-document. Likewise, use `git cherry-pick --no-commit` or
`git revert --no-commit` before creating the resulting commit through this
same form. A true fast-forward creates no commit or message and needs no
here-document.

Do not use `-m`, `-C`, `-c`, an editor-composed message, `-F` with a temporary
message file, an unquoted here-document delimiter, shell variables, or command
substitution as the commit-message source. The quoted delimiter is required so
backticks, dollar signs, and other message text cannot be evaluated by the
shell.


## Describe the Changes

Use an imperative, capitalized subject and a self-contained body. After the
subject, every commit body must contain these five nonempty sections in this
exact order:

```text
Before:
<prior behavior or state>

After:
<new behavior or state and why it matters>

Planned Actions:
<unfinished work this worker, Codex, or another named automated worker is
expected to perform without new human authority or information; write "None."
when no such work remains>

Intervention Needed:
<decisions, approvals, credentials, unavailable information, physical actions,
or other outside help required from a human; identify who must act and what the
action unblocks; write "None." when no intervention is currently required>

Details:
<per-file changes, checks and results, provenance, and merge resolution>
```

The reader must be able to evaluate the change without opening the
changed files, looking up project vocabulary, or reconstructing
context from earlier commits.

### Required: Describe Content, Not Process

Do not write commit messages that only describe process, history, or
workflow. The commit message must describe the change in product
behavior, failure behavior, a rule, data meaning, a documentation
requirement, or a test case.

This is a hard gate, not a style preference.

The subject and the opening sentence of both `Before:` and `After:` must say
what changed in product behavior, failure behavior, a rule, data meaning, a
documentation requirement, or a test case. Every per-file entry in `Details:`
must do the same for that file. Rewrite the message if a reader can identify
only that work was merged, reviewed, checkpointed, integrated, updated,
synchronized, processed, or supported by evidence, but cannot identify what
the software now does or what the repository now requires.

Process words may state necessary provenance later in the message. They may
also appear in a merge subject because the source branch name is required, but
the same subject must name the material code, product behavior, or requirement
being combined. Use observable behavior and concrete requirements:

```text
Bad:  Record the changed code
Good:  The widget now ignores the `foo` parameter when `bar` is true

Bad:  Integrate storage recovery work
Good: Recover the stored result after restart without repeating the external action

Bad:  Update publication tests
Good: Reject publication when any linked data block is missing

Bad:  Merge vcs-file-tree into merge-queue
Good: Merge vcs-file-tree verified byte-range reads into merge-queue

Bad:  Update the deployment plan
Good: Require missing-data and restart-without-repetition tests before live use
```

For documentation and planning changes, name the decision, requirement,
schedule, factual correction, or open task that the text adds or changes. Do
not claim that “documentation was updated” as the result. For tests, state the
failure or success case now exercised.

1. Make the subject state the concrete effect of the change. Do not use a
   workflow-status subject such as "record evidence," "advance qualification,"
   or "update candidate" when the actual effect can be named.
   For a merge commit, include the exact source branch name in the subject.
   Capture that name when reviewing the merge; do not substitute only a commit
   object ID or a generic phrase such as "peer history."
2. In `Before:`, state what the program, document, plan, or repository did or
   required before the commit. Give enough concrete context to understand the
   problem without consulting earlier history.
3. In `After:`, state what it does or requires after the commit and why the
   difference matters. Describe the actual behavior or requirement before
   discussing how the
   change arrived from another branch, how it was reviewed, or where evidence
   is stored. Put Git provenance and integration mechanics later in the body.
4. In `Planned Actions:`, state unfinished work that this worker, Codex, or
   another named automated worker is expected to perform without new human
   authority or information. Name the responsible worker when it matters.
   Write `None.` when no such work remains; never omit or leave this section
   empty.
5. In `Intervention Needed:`, state only decisions, approvals, credentials,
   unavailable information, physical actions, or other outside help required
   from a human. Identify who must act and what the action unblocks. Write
   `None.` when no intervention is currently required; never omit or leave
   this section empty. Keep limitations, failed or omitted checks, and risks
   visible with the planned action that addresses them, or in `Details:` when
   no planned mitigation exists. Do not classify ordinary worker follow-up as
   human intervention.
6. In `Details:`, list every changed file separately and explain that file's
   role and exact before-and-after change. Then state concrete check commands or
   situations, expected results, actual results, provenance, and merge
   resolution as applicable.
7. Replace specialized terms with ordinary descriptions. When a specialized
   term is unavoidable, define it at first use in the commit message itself.
   A term's presence in `GLOSSARY.md` does not make the commit message
   self-contained. Expand acronyms unless their meaning is universal and
   unambiguous in the message.
8. State concrete failure behavior. For example, say "if one linked data block
   is missing, publish nothing" rather than "preserve incomplete-closure
   safety." Say what a receipt identifies and what is recovered rather than
   saying only "receipt-backed restart."
9. Do not use empty descriptions such as "record exactly what the experiment
   proves" or "preserve the barriers"; state the result and the remaining
   restriction.
10. Explain checks in the same way: name the situation exercised, the expected
   result, and the actual result. A check name or `PASS` label alone is not an
   explanation.
11. When a proquint-qualified item is relevant, describe what the item governs
   in ordinary language instead of mentioning only its identifier.

Before committing, read the message as a reviewer unfamiliar with the current
task. Rewrite it unless all five required sections are present in order and the
message alone answers: What changes? Why? What happens when it fails? What is
still not done?

For example, prefer this structure:

> Before:
> The deployment plan did not name the two storage failures that must be tested
> before the storage code can be used.
>
> After:
> The plan requires one test that removes a linked data block and verifies that
> no partial shipment list or storage receipt becomes visible, plus one restart
> test that recovers the exact stored result without repeating the external action.
>
> Planned Actions:
> The on-disk storage format is not selected, and the storage code is not yet
> implemented.
>
> Intervention Needed:
> None.
>
> Details:
> - The deployment TODO now names both tests and their exact success conditions.
> - No executable check ran because this commit changes only the plan.

Do not replace that explanation with "record focused launch-storage evidence."
