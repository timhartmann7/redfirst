# Rules for the agents working in your repository

`redfirst` refuses a diff that breaks its rules. It cannot make an agent write a
better one. Telling the agent the rules up front costs nothing, cuts the share
of violations on the first attempt, and saves the tokens a retry would burn.

So put the block below into the file your agent reads: `CLAUDE.md`, `AGENTS.md`,
`.cursor/rules`, whatever your tool looks for. Keep that file in
`paths.protected`, which the built-in defaults already do for the common names.

Two things this page is not.

It is not the install. Do not ask an agent to download `redfirst` and wire up
CI. The product claims an agent cannot influence what judges it, and an
agent-driven install refutes that claim at install time. See
[the README](../README.md#do-not-install-this-through-your-agent).

It is not a substitute for the binary. Every line below is a request the agent
may ignore, which is exactly why the binary exists.

## The block

Paste this, then edit the numbers and paths to match your own `redfirst.toml`.
`redfirst explain` prints the set that actually applies, and `redfirst explain
--path src/lib/order/total.ts` prints it for one file.

````markdown
## What CI checks on every pull request

A tool called `redfirst` judges this branch against the rules below and blocks
the merge when one of them breaks. It reads the rules and the test hooks from
the base branch, so editing them on this branch changes nothing about how this
branch is judged. The CI workflow is on the protected list below, so touching it
is a refusal on its own. Do not try.

**Do not touch these paths.** `.redfirst/**`, `redfirst.toml`,
`.github/workflows/redfirst.yml`, `CLAUDE.md`, `AGENTS.md`, `.mcp.json`,
`.claude/**`, `.cursor/**`. A change to any of them is a refusal, whatever else
the diff does. If the task cannot be finished without one, stop and say so.

**Write the test first, and make sure it fails.** The fix ships with a test
that is red before the fix and green after it. CI runs your new test cases
against the base branch with your fix left out, three times over. A case that
passes there does not cover the bug, and the run is refused. A case that passes
sometimes is refused too: an unreliable regression test is worth nothing.

**Do not remove existing insurance.** An existing test case may not disappear
and may not go red. Renaming a symbol that tests cover is fine, the case names
survive it. Deleting a test, emptying its body, or loosening its assertions to
get a green run is not.

**Do not disable tests.** No `.skip(`, no `.only(`, no `xit(`, no `xdescribe(`
added anywhere in a test file.

**Do not edit the test surface to fit the output.** Snapshots, golden files,
`testdata`, mocks, `conftest.py`, the test runner config. Touching one of them is
a refusal on its own, and adding a test case beside it does not clear that. If a
snapshot is genuinely stale, say so in the pull request description rather than
quietly regenerating it.

**Keep the diff small.** Roughly 8 files, 300 added lines, 120 deleted lines,
tests excluded from the count. A larger change belongs in several pull
requests. Say so instead of squeezing it into one.

**Do not silence errors.** No empty `catch` blocks, no `@ts-ignore`, no
`eslint-disable`, no widening a type to `any` to get past a compiler error.
These end up in the report for a human to read even when they do not block.

**The whole suite has to pass on your branch.** A test you did not touch that
was already broken is not your problem, and CI tells the two apart. A test you
touched is.

When a rule leaves no legal move, stop and explain the conflict. A refusal you
predicted costs one comment. A refusal CI finds costs a full run.
````

## Adjusting it

The block above matches what `redfirst init --config` generates: the strict set
meant for branches an agent writes. The built-in defaults are wider, because
they judge human pull requests, and a few lines change with them.

| If your config says | Change the block to say |
|---|---|
| `require_new = false` (the default) | Drop nothing. A fix still needs a test, and no gate demands one on its own |
| `immutability = "append-only"` | An existing test file may gain lines and may not lose any. Renaming a covered symbol needs a human |
| `immutability = "strict"` | An existing test file may not be edited at all |
| `fixtures_immutability = "warn"` (the default) | A snapshot edit reaches the report and does not block |
| Your own `paths.protected` | The path list, verbatim. `redfirst explain` prints it |
| Your own `[budget]` | The three numbers |
| No `[severity]` section (the default) | An oversized diff is a report line rather than a refusal. Keep the numbers and drop the sentence about splitting the pull request |

Do not describe rules you have not switched on. An agent that meets a check it
was never warned about retries blindly; an agent warned about a check that does
not exist wastes its budget avoiding a ghost.
