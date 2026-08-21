# `redfirst.toml`

The rules one repository is judged by. The file is optional: without it the
built-in defaults apply, and that is the normal mode for tiers 0 and 1 rather
than a degradation.

Two facts decide everything else on this page.

**The file is read from the base ref, never from the branch under judgement.**
An agent that edits `redfirst.toml` on its own branch changes nothing about how
that branch is judged. Keep the path in `paths.protected` so the edit is a
refusal instead of a quiet no-op, and the defaults already do.

**A missing file is not an error. A broken file is.** Ignoring a typo would be
worse than refusing: a config with `protected_paths` instead of `protected`
would turn into an empty denylist and nobody would see it. So an unknown key is
exit code 3, and so is every other kind of invalid file. The list is in
[What gets refused](#what-gets-refused).

## The smallest useful file

```toml
version = 1

[paths]
protected = [
  ".redfirst/**", "redfirst.toml", ".github/workflows/redfirst.yml",
  "CLAUDE.md", "AGENTS.md", ".mcp.json", ".claude/**",
  "src/lib/server/billing/**",
]

[tests]
immutability = "cases"
require_new = true
```

Everything not named here keeps its built-in value. `redfirst explain` prints
the result, which is the answer to "what is this branch actually judged by".

`redfirst init --config` generates a fully commented file with the strict values
meant for agent branches. See [The generated
template](#the-generated-template).

## How your file joins the defaults

**The unit of replacement is the key, not the section.** A key you set replaces
its default whole. A key you leave out keeps its default, including keys in a
section you touched.

```toml
[paths]
allowed = ["src/**"]
```

Here `paths.protected` and `paths.guarded` still hold their built-in lists. If
section-level replacement were the rule, those two lines would have emptied the
denylist in silence, and a formally valid config would have opened the hole that
validation exists to close.

**Lists never merge.** A `protected` you set replaces the built-in list rather
than extending it. To get the defaults plus your own entries, write both out.
`protected = []` is how you get an empty denylist, and it is the only way.

**`[severity]` is the exception, because it is a table of gate ids rather than a
list.** Setting `severity.protected-paths` leaves the default
`severity.diff-budget = "warn"` in place.

## Globs

Patterns are gitignore-style globs, matched by
[doublestar](https://github.com/bmatcuk/doublestar) against the repository-root
relative path.

- `**` crosses directory boundaries: `src/**` matches `src/lib/order/total.ts`.
- A pattern with **no** `/` in it matches the base name at any depth. `CLAUDE.md`
  matches `docs/CLAUDE.md` as well as `CLAUDE.md`, and `*.snap` matches every
  snapshot in the tree.
- A trailing `/` is trimmed, so `src/` and `src` mean the same thing. Neither
  matches what is inside the directory; write `src/**` for that.
- A pattern that does not compile is exit code 3, and so is an empty string in a
  pattern list.

Regular expressions (`tests.forbid_disabled`, `suppression.patterns`) are Go
regexes, matched against a single added line at a time. TOML basic strings eat
backslashes, so write them as literal strings in single quotes or escape twice:
`'\.skip\('` and `"\\.skip\\("` are the same expression.

## Reference

### `version`

| Key | Type | Default |
|---|---|---|
| `version` | integer | `1` |

The config format. Anything but `1` is exit code 3. Leaving it out keeps `1`,
but write it: the day version 2 exists, a file without it is a file nobody can
date.

### `[budget]`

The `diff-budget` gate. Files matching `tests.patterns` and files marked
`linguist-generated` in `.gitattributes` are excluded from all three counts, so
the gate never charges an agent for the tests a neighbouring gate demands. A
fixture counts unless it also matches `tests.patterns`: `data.golden` counts,
while `total.test.js.snap` matches `**/*.test.*` and is excluded. A binary file
counts as one file and zero lines.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `budget.max_files` | integer | `20` | Changed files allowed |
| `budget.max_added_lines` | integer | `800` | Added lines allowed |
| `budget.max_deleted_lines` | integer | `400` | Deleted lines allowed |

The default severity of this gate is `warn`, so an overrun is a report line
rather than a block. Set `severity.diff-budget = "fail"` to make it bite.

Do not guess your numbers. `redfirst audit --last 50 --per-pr --report csv`
prints the distribution over your own history, and its upper quartile is a limit
nobody argues with.

### `[paths]`

| Key | Type | Default |
|---|---|---|
| `paths.protected` | list of globs | `[".redfirst/**", "redfirst.toml", ".github/workflows/redfirst.yml", "CLAUDE.md", "AGENTS.md", ".mcp.json", ".claude/**", ".cursor/**"]` |
| `paths.guarded` | list of globs | `[".github/**", "Dockerfile*", "docker-compose*.yml", "**/migrations/**", "**/*.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum"]` |
| `paths.allowed` | list of globs | empty |

**`protected` blocks.** Any diff touching one of these paths is refused with
`remediation: revert-paths`. Additions, modifications, deletions and both ends
of a rename all count.

The default list is short on purpose. It holds the files whose edits change the
rules for the next agent: the hooks, the config, the workflow that runs
redfirst, and the instruction files agents read. Infrastructure, migrations and
lockfiles are in `guarded` instead, because a gate that fires on an ordinary
human pull request gets the tool deleted rather than the diff fixed.

**`guarded` reports.** A file matching one of these produces a `guarded-paths`
line in the report and leaves the verdict alone. A human who bumped dependencies
sees a line and merges; an agent that went into the migrations catches a
reviewer's eye.

**`allowed` is an allowlist, off by default.** When it is non-empty, every file
in the diff has to match it, and one that does not is refused. `protected`
outranks it: a path in both lists is still refused.

The same pattern in `protected` and `guarded` is exit code 3, rather than a
silent decision in somebody's favour. Overlapping patterns that are not spelled
identically are fine, and the defaults rely on that: `.github/**` is guarded
while `.github/workflows/redfirst.yml` is protected.

### `[tests]`

| Key | Type | Default |
|---|---|---|
| `tests.patterns` | list of globs | `["**/*.test.*", "**/*.spec.*", "**/*_test.go", "**/test_*.py", "tests/**", "spec/**", "__tests__/**"]` |
| `tests.fixtures` | list of globs | `["**/__snapshots__/**", "**/*.snap", "**/*.golden", "**/testdata/**", "**/fixtures/**", "**/__mocks__/**", "**/conftest.py", "**/setupTests.*", "**/*.setup.ts", "**/*.setup.js", "jest.config.*", "vitest.config.*", "playwright.config.*"]` |
| `tests.immutability` | `strict`, `append-only`, `cases` | `append-only` |
| `tests.fixtures_immutability` | the same three plus `warn` | `warn` |
| `tests.require_new` | boolean | `false` |
| `tests.forbid_disabled` | list of regexes | `['\.skip\(', '\.only\(', '\bxit\(', '\bxdescribe\(']` |
| `tests.known_flaky` | list of strings | empty |

**`patterns` are the files that hold test cases.** They drive
`test-immutability`, `new-test-present`, `no-disabled-tests` and the budget
exclusion.

**`fixtures` are the test surface: files that decide test outcomes without
holding cases.** Snapshots, golden files, `testdata`, mocks, `conftest.py`, the
runner config. Editing a snapshot to match new output is the cheapest way to
turn a suite green while fixing nothing, and without this list it walks past
every gate: such a file matches no test pattern, sits in no denylist, and holds
no `.skip` to find.

A fixture in the diff also brings `red-green` into play, because the gate probes
every file of the test surface. A diff that touches one and adds no new test
case is refused with `test files changed but no new test case appeared`, which
is what stands between a repository and a snapshot quietly rewritten to match
wrong output.

A file matching both lists is judged as a fixture when it is edited, which is
what makes `total.test.js.snap` answer to `fixtures_immutability` rather than to
`immutability`: whoever enumerated the narrower surface meant that surface. A
file that disappears answers to both lists, and the mode that is not `warn`
decides, because deleting a test is the violation the whole tool exists for.

**The immutability modes.**

| Mode | Rule | Needs |
|---|---|---|
| `strict` | An existing file is neither modified nor deleted | the diff |
| `append-only` | Modification allowed while the file's diff deletes no line | the diff |
| `cases` | No case that existed on base disappeared or went red on head | hooks and per-case names |
| `warn` | The edit reaches the report and leaves the verdict alone | the diff |

`warn` is legal for `fixtures_immutability` only. Setting
`tests.immutability = "warn"` is exit code 3.

`cases` exists because the first two forbid legitimate refactoring: renaming a
covered function needs the old name to leave the test file, which `strict`
refuses outright and `append-only` refuses on the deleted lines. `cases` judges
outcomes instead, so the rename passes and a hollowed-out test does not. It
misses a weakened assertion inside an existing case, which `append-only` catches
on the deleted lines. The two go blind in different places, which is why both
stay.

Without hooks, `cases` downgrades to `append-only` and the report says so:

```
WARN  config                 tests.immutability "cases" needs hooks, downgraded to "append-only"
```

The downgrade moves toward strictness. `append-only` forbids more than `cases`
does, so nothing gets weaker when the hooks are missing.

**`require_new`** demands that the diff add at least one line to a file that
holds test cases. It is off in the defaults: on tier 1 the gate faces human pull
requests, where a refactor with no new test is a legitimate diff. Whoever judges
agent branches switches it on, and `redfirst init --config` does.

**`forbid_disabled`** is matched against added lines in files under
`tests.patterns`, in every file rather than only in new ones: the other gates
permit appending to an existing test file, and `.skip(` is exactly what would be
appended.

**`known_flaky`** takes a test out of `suite-green` entirely. An entry matches a
case name or a file path, spelled exactly as your runner reports it. The list
comes from base, so the branch under judgement cannot extend it.

### `[red_green]`

| Key | Type | Default |
|---|---|---|
| `red_green.probe_runs` | integer | `3` |
| `red_green.compile_failure_on_base` | `red`, `weak-red`, `fail` | `weak-red` |
| `red_green.enabled` | boolean | `true` |

**`probe_runs`** is how many times each probe runs, on base and on head alike.
Every run has to return the same result. This is the one place in the system
where randomness produces a pass: a fake test that sits green on base gets
stamped as a real regression test on one random red. Rerunning until the answer
suits us would bend the result, so the gate demands the same answer every time
instead. `--probe-runs N` overrides the value for one run. A value below `1` is
exit code 3.

**`compile_failure_on_base`** covers the new test that imports a function base
does not have yet. The run then dies before the assertions and no case names
appear.

| Value | Behaviour |
|---|---|
| `weak-red` | Count the file set as red, judge by file, and say so in the report |
| `red` | The same, without the report line |
| `fail` | Refuse: `the added tests do not build on base, and the config counts that as a violation` |

Under `weak-red` and `red` the check degenerates to file level, and the report
has to say so, because "the code does not compile without the fix" claims less
than "the test catches the bug".

**`red_green.enabled = false` switches the gate off**, exactly as
`gates.disabled = ["red-green"]` does. The two are the same switch: `redfirst
explain` prints the gate as disabled either way, and the report line reads
`SKIP  red-green  disabled in config`.

### `[suite]`

| Key | Type | Default |
|---|---|---|
| `suite.retry_failed` | integer | `1` |

How many times `suite-green` reruns the failed tests before classifying them. A
value below `1` skips the retry, and the gate then has no way to tell a flake
from a failure the repository already had: every untouched failure costs a full
suite run on base, and one that was green there ends as a refusal.

### `[suppression]`

| Key | Type | Default |
|---|---|---|
| `suppression.enabled` | boolean | `true` |
| `suppression.patterns` | list of regexes | `['catch\s*\(\s*\w*\s*\)\s*\{\s*\}', '@ts-ignore', '@ts-expect-error', 'eslint-disable', ':\s*any\b']` |

Added lines matching these land in the `REVIEW NEEDED` block of the report. The
gate cannot fail and carries no remediation. An empty catch inside an autofix
almost always marks a silenced symptom, and a human is the right reader for
that.

The patterns are language-specific by nature. A Go or Python repository should
replace them, since the defaults look for TypeScript.

### `[severity]`

| Key | Type | Default |
|---|---|---|
| `severity.<gate-id>` | `fail` or `warn` | `fail` for every gate except `diff-budget`, which is `warn` |

`warn` turns a refusal into a report line: the status becomes `WARN`, the
remediation is dropped, and the gate stops contributing to the exit code.

Lowering `red-green` or `suite-green` is refused with exit code 3. Those two
carry the product's claim, and a config that could switch them off would make
the verdict mean nothing. Naming a gate that does not exist is exit code 3 as
well.

### `[gates]`

| Key | Type | Default |
|---|---|---|
| `gates.disabled` | list of gate ids | empty |

A disabled gate reports `SKIP` with the reason `disabled in config`. Ids are
words rather than codes: `disabled = ["blast-radius"]` reads without a
dictionary. An unknown id is exit code 3.

The nine ids: `protected-paths`, `diff-budget`, `test-immutability`,
`new-test-present`, `no-disabled-tests`, `suppression-scan`, `suite-green`,
`red-green`, `blast-radius`.

### `[scopes]` and `[[scope]]`

| Key | Type | Default |
|---|---|---|
| `scopes.allow_cross_scope` | boolean | `true` |

v1 parses the scope shape and implements none of it: a non-empty `[[scope]]`
list is exit code 3 with `scopes are not implemented yet`. The shape is frozen
now so that adding scopes later breaks nobody. `scopes.allow_cross_scope` is
parsed and read by nothing in v1: with no scopes to cross, it has nothing to
decide.

## What gets refused

Every row here is exit code 3, and every one of them means "a human wrote a
config that does not say what they think it says". A retry cannot fix any of
them, which is why exit 3 is separate from exit 1.

| Situation | What the message says |
|---|---|
| No file | Not a refusal. Built-in defaults, `config=defaults` in the header |
| Invalid TOML | The parser's own line, and the last key it read |
| Unknown key | `unknown key(s): paths.protected_paths` |
| `version` is not 1 | `version = 2, this binary reads version = 1` |
| Non-empty `[[scope]]` | `scopes are not implemented yet` |
| `severity.red-green` or `severity.suite-green` set to `warn` | `severity.red-green = "warn" is forbidden, these two gates block or nothing does` |
| `severity` names an unknown gate | `severity names an unknown gate "red_green"` |
| `gates.disabled` names an unknown gate | `gates.disabled names an unknown gate "blast_radius"` |
| The same pattern in `protected` and `guarded` | `pattern "**/*.lock" sits in both paths.protected and paths.guarded` |
| `tests.immutability = "warn"` | `tests.immutability = "warn" applies to tests.fixtures_immutability only` |
| `red_green.probe_runs` below 1 | `red_green.probe_runs = 0, a probe has to run at least once` |
| A glob that does not compile | `invalid glob pattern "src/[abc"` |
| An empty string in a pattern list | `empty glob pattern` |
| A regex that does not compile | `invalid regular expression "([a-": ...` |
| An unknown enum value | `unknown immutability mode "append_only"` |

A run that cannot read the file at all, because git failed or the object is
missing, is exit code 2 instead: that is a broken repository rather than a
broken config.

## Seeing the result

`redfirst explain` prints the effective rule set, and it is the fastest way to
settle an argument about a verdict.

```
$ redfirst explain --base origin/main
redfirst v0.1.0 · explain · config=base:redfirst.toml

budget.max_files                   8
budget.max_added_lines             300
...
tests.immutability                 cases
tests.fixtures_immutability        strict
```

`redfirst explain --path <file>` answers for one file, which is what you want
when a gate refused a path and nobody can see why:

```
$ redfirst explain --path src/total.test.ts
redfirst v0.1.0 · explain · config=defaults

path               src/total.test.ts
paths.protected    no
paths.guarded      no
paths.allowed      (empty) · the diff may touch any path
tests.patterns     yes · **/*.test.* · tests.immutability = append-only applies
tests.fixtures     no
diff-budget        not counted · a test file never counts against the budget
no-disabled-tests  added lines scanned for \.skip\(, \.only\(, \bxit\(, \bxdescribe\(
```

Both read the config from `--base`, the same ref `verify` reads it from. A
config sitting in your working tree and not on base decides nothing, and this is
where you find that out.

## The generated template

`redfirst init --config` writes a commented `redfirst.toml` whose lists come
from the built-in defaults and whose seven other values are the strict set for
agent branches:

| Key | Generated | Built-in default |
|---|---|---|
| `budget.max_files` | `8` | `20` |
| `budget.max_added_lines` | `300` | `800` |
| `budget.max_deleted_lines` | `120` | `400` |
| `tests.immutability` | `cases` | `append-only` |
| `tests.fixtures_immutability` | `strict` | `warn` |
| `tests.require_new` | `true` | `false` |
| `severity.diff-budget` | `fail` | `warn` |

The split is deliberate. The defaults judge human pull requests and stay out of
the way; the template judges an agent. A snapshot rewritten to match wrong
output, a test file hollowed out, a twenty-file diff for a one-line fix: all
three are ordinary on a human branch and almost always mean bending the result
on an agent one.

`init` refuses to write over an existing file. Move yours aside first, or copy
the pieces you want out of the generated one.

## One config, no inheritance

There is one file, at the repository root, and it does not inherit from
anything. A monorepo puts the differences between packages into `[[scope]]`
sections of that same file, once v2 implements them.

The reason is not simplicity. The config marks the boundary of an agent's
authority, and one line in CODEOWNERS guards a centralised file. Forty files
scattered across package directories are harder to guard, and slipping a quiet
relaxation into one of them is easier. Config inheritance is also where ESLint
and tsconfig trip, because the semantics of merging lists stay ambiguous, and
this file answers that question by never merging them.
