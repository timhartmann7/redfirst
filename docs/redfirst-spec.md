# Spec: `redfirst`, a deterministic diff judge for CI

> The name states the thesis: no test is born green. Before publishing, check that the GitHub org, the domain and the module path on pkg.go.dev are free.

**Implementation language:** Go, latest stable release
**License:** MIT
**Artifact:** one static binary, no runtime and no external dependencies on the target machine

---

## 1. Why this exists

An agent edits code in a branch. Someone has to rule on whether that diff obeys the rules that make it worth showing a human at all.

`redfirst` issues that ruling. It runs in CI, looks at the diff of a branch against base, runs a set of deterministic checks and returns an exit code. It writes nothing into the repository, deploys nothing, calls no LLM.

The key property: **all code that judges comes from the base branch, all code being judged comes from the head branch.** The agent cannot soften the rules it is checked against, because its edits to the config and the hooks are ignored.

The core does not know who wrote the diff. A diff from Claude Code in a terminal, from an autonomous agent in a sandbox, and from a human typing by hand look identical to it. The autofix loop is the first consumer, not the scope.

### What `redfirst` does not do

The boundaries matter more than the features. Everything below is out of scope by choice:

- it does not orchestrate an agent and does not launch one;
- it does not open or merge PRs;
- it does not deploy;
- it calls no models and contains no LLM-based heuristics;
- it does not judge whether the fix is **correct**, only whether the rules hold;
- it knows nothing about specific languages and frameworks: TypeScript, Python and Go are glob patterns and shell scripts to it.

The last point deserves saying out loud. `redfirst` catches "the agent deleted a test", "the agent touched the migrations", "the agent wrote a test that stays green without the fix". It does not catch "the agent wrapped everything in try/catch, the test goes red before and green after, and the feature is dead". A human reviewer owns that case, and no deterministic check will ever close it.

---

## 2. Threat model

We defend against **a lazy agent, not an attacker.**

The difference is practical. An agent will not hunt for a bypass through symlinks, filesystem races or swapped git objects. An agent takes the cheapest path: disable the test, widen its own denylist, add `.skip`, wrap the failing call in try/catch. We tune the checks for that.

Access rights guard the perimeter, not the binary:

| Threat | What closes it |
|---|---|
| Agent rewrites config or hooks to suit itself | `redfirst` reads both from base |
| Agent disables the CI job | Workflow in protected paths plus branch protection |
| Agent swaps the binary version | Version and checksum pinned in the workflow, workflow protected |
| Agent merges itself | The agent token has no merge rights |
| Agent rewrites instructions for future agents | `CLAUDE.md`, `AGENTS.md`, `.mcp.json` and skill directories sit in protected paths by default |
| Head code executes during verification | Verification runs only in a throwaway runner with no production access |

The second-to-last row is quieter than the rest and more dangerous. An edit to `CLAUDE.md` changes the behaviour of every future agent in the repository while looking like a harmless documentation tweak.

The last row is mandatory. Hooks from base run tests, tests execute code from head. Arbitrary agent code runs during verification, and that is unavoidable. So the verification runner gets no production credentials and no access to the production network.

### On the side: the install path must not contradict the product

Installing `redfirst` through the agent ("ask Claude Code to download the binary and set up CI") is forbidden and unsupported. The product thesis says the agent cannot touch what judges it. An agent-driven install refutes that thesis at install time, makes installations non-deterministic, and reads as a supply chain attack.

What you can and should distribute through `CLAUDE.md` is the **rules**, not the install: no edits to existing tests, a red test on main required, the list of protected paths, the diff budget. That is a request the agent may ignore, which is why the binary exists. The README must carry this wording as it stands.

---

## 3. Invariants

Keep them in mind for every implementation decision. On conflict, the earlier one wins.

1. **A stateless core.** `redfirst` is a pure function of `(repo, base, head)`. No cache between runs. A judge with memory is a judge with cache invalidation, and a stale entry gives a silently wrong verdict.
2. **Err toward refusal only.** Any ambiguity resolves to `1` or `2`, never to `0`. A false accusation costs one retry, a false acquittal ships to production.
3. **Separate "the agent is at fault" from "the repository is broken".** Exit code `1` against exit code `2`. Mix them and the orchestrator burns its retry budget on someone else's flake.
4. **An incomplete set of gates is a valid set.** A missing config or missing hooks narrow the scope of the check without turning the verdict into a lie: what got checked got a real check, and the rest carries an explicit unavailable marker.
5. Config, hooks and rule lists are read **only** from the base ref.
6. `redfirst` writes nothing except the report file and stdout/stderr.
7. The same input yields the same verdict. No network activity, no reads of the clock or of randomness inside gate logic.
8. Teardown always runs, including on panic and on timeout.
9. The report explains **what exactly** was violated and **where**, never "check failed".

---

## 4. Adoption tiers

The barrier to entry is the main risk to this project. A tool that demands a binary, a config, three scripts and branch protection setup will not survive first contact. So adoption splits into three tiers, and **nothing gets rewritten between them**: each next tier adds files that the binary discovers on its own. The CI command on tier 1 and tier 2 is the same command.

| | Tier 0 | Tier 1 | Tier 2 |
|---|---|---|---|
| Files added to the repo | none | one workflow | workflow plus 2 to 4 scripts |
| Config | not needed | not needed | optional |
| Tests execute | no | no | yes |
| When it runs | by hand | on every PR | on every PR |
| Active gates | all static ones (report mode) | all static ones | all |
| Catches a fake test | no | no | **yes** |
| Time to adopt | a minute | 10 minutes | 20 minutes and up |

### Tier 0, local audit

`redfirst audit --last 20` reads the history of the last N merged PRs, applies the static gates to their diffs and prints a summary. It executes nothing, installs nothing, and adds no file to the repository.

```
Audited 20 merged PRs (last 34 days)

  6  modified an existing test file
  2  added .skip() to a test
  4  exceeded the default diff budget (>20 files)
  1  touched .github/workflows
  3  added an empty catch block
```

This tier exists for recognition, not protection. A reader gets an unexpected fact about their own repository within a minute. The same command supplies ecosystem statistics for the public launch.

### Tier 1, static gates in CI

One workflow, no config, built-in defaults apply. Gates that need no test execution block the PR. `red-green` and `suite-green` come back as `unavailable` with the reason `no hooks`, and the verdict on the rest stays complete.

### Tier 2, hooks and the red-green check

A `.redfirst/` directory appears in the repository. The workflow does not change. The binary discovers the hooks and switches on `red-green` and `suite-green`.

The cost of this step depends on project maturity, not on language. For a project with pure unit tests, all of `test.sh` fits on one line. For a project with a database, migrations and seeds, expect a few hours to a day, and that work pays off with or without `redfirst`.

### Consequences for the implementation

- The config is **optional**. Its absence is not an error (see section 6).
- The hooks are **optional**. Their absence moves dependent gates to `unavailable` rather than to a refusal.
- Defaults are compiled into the binary and are as unswappable as the config from base: the workflow pins the binary version, and the workflow sits in protected paths.
- Diagnostics must be concrete. People drop off because they cannot tell what to fix, not because their stack is unsupported. `redfirst doctor` owns that job.

---

## 5. CLI contract

```
redfirst verify [flags]      # judge one diff
redfirst audit  [flags]      # tier 0: static gates over PR history
redfirst init   [flags]      # generate workflow, config, hook stubs
redfirst doctor [flags]      # what works now, what does not, and why
redfirst explain [flags]     # effective rules, runs nothing
redfirst version
```

### `redfirst verify`

| Flag | Default | Description |
|---|---|---|
| `--base <ref>` | `origin/main` | Reference branch. Source of config, hooks and rules |
| `--head <ref>` | `HEAD` | The branch under judgement |
| `--repo <path>` | `.` | Path to the git repository |
| `--config <path>` | `redfirst.toml` | Config path **inside the base ref**. A missing file is not an error |
| `--gates <ids>` | all available | Restrict the gate set, comma separated |
| `--report <fmt>` | `text` | `text`, `json`, `both` |
| `--out <path>` | stdout | Where to write the report |
| `--timeout <dur>` | `30m` | Global deadline for the whole run |
| `--work-dir <path>` | system temp | Where to create worktrees |
| `--no-hooks` | false | Force static gates only |
| `--fresh-env` | false | Forbid service reuse |
| `--probe-runs <n>` | from config | Override the number of probe runs in `red-green` |

### `redfirst audit`

| Flag | Default | Description |
|---|---|---|
| `--last <n>` | `20` | How many recent history units to analyse |
| `--since <date>` | none | Alternative to `--last` |
| `--base <ref>` | `origin/main` | What things were merged into |
| `--unit <mode>` | `auto` | What counts as a unit: `auto`, `merge`, `squash`, `commit` |
| `--report <fmt>` | `text` | `text`, `json`, `csv` |
| `--per-pr` | false | Print a breakdown per unit, not only the summary |

Works with static gates only and reads only. It never brings up an environment, even when hooks exist.

#### The unit of history

Reconstructing "the last N merged PRs" without a forge API depends on how the repository merges. The binary picks a mode from a sample of the last 50 first-parent commits on `--base` and prints it in the report header.

| Mode | Signal | Unit | Diff of a unit |
|---|---|---|---|
| `merge` | More than half of first-parent commits have two parents | Merge commit | `git diff $(git merge-base P1 P2) P2` |
| `squash` | More than half of subjects contain `(#\d+)` | First-parent commit | `git show --numstat` on the commit |
| `commit` | Neither | First-parent commit | Same |

`squash` and `commit` differ in one respect: the first puts a PR number in the report. Their accuracy is identical and lower than `merge`. A PR rebase-merged as three commits counts as three units, and the `diff-budget` statistics come out understated. The report header must say so rather than keep quiet:

```
Audited 20 squashed commits on origin/main (last 34 days)
  unit=squash · a PR merged as several commits counts several times
```

Mode `merge` gives exact PR boundaries and is the one to prefer, but you cannot choose it: someone else's repository settings decide. v1 makes no forge API call for the PR list. Such a call breaks "zero network at runtime" and demands a token at the step we sell as "one command, nothing installed".

### `redfirst init`

| Flag | Description |
|---|---|
| `--ci` | Generate `.github/workflows/redfirst.yml` with a pinned version and SHA256 |
| `--config` | Generate `redfirst.toml` from built-in defaults, with comments |
| `--hooks <preset>` | Drop templates into `.redfirst/`: `node-unit`, `node-postgres-docker`, `python-pytest`, `go`, `blank` |

Deterministic template generation, no model calls and no network. The binary picks the preset from repository markers unless you name one.

### `redfirst doctor`

Prints the current adoption state and what blocks the next tier:

```
redfirst doctor · tier 1

  config      not found → using built-in defaults
  hooks       not found → red-green, suite-green unavailable
  detected    node + vitest, no docker-compose

  To reach tier 2:
    redfirst init --hooks node-unit
```

Diagnostics must name the specific problem instead of the fact of failure: `test.sh ignores $REDFIRST_FILTER, probes will run the whole suite three times` rather than `hook error`.

### `redfirst explain`

| Flag | Description |
|---|---|
| `--path <file>` | Print the effective rules for one path |
| `--base <ref>` | Where to read the config from |

You need it when adopting a repository and when arguing about a verdict.

### Exit codes

| Code | Meaning | Orchestrator reaction |
|---|---|---|
| `0` | Every available gate passed | PR goes to human review |
| `1` | Gate violation, the agent has a legal move | Retry with the report in the agent context, attempt counter +1 |
| `2` | Harness error, or the repository was broken before the agent | Retry without penalty, alert a human after N |
| `3` | Configuration error: the config exists but is invalid | Alert a human at once, a retry is pointless |
| `4` | Internal `redfirst` error | Alert, file an issue on the core repository |
| `5` | Violation with no legal move for the agent | Alert a human at once, a retry is pointless |

Splitting `1` from `2` is critical. An orchestrator that blames the agent for a failed docker-compose burns the retry budget on an infrastructure problem.

Splitting `1` from `5` closes the second budget leak. Some refusals survive any diff the agent can write: the harness returns no test case names, so `red-green` cannot verify the cases added to a modified test file, and rewriting the fix changes nothing. Without code `5` the orchestrator hits that wall N times and escalates a task that was valid from the start.

A missing config is **not** exit code `3`.

### Remediation hint

Every gate result carries a `remediation` field, a machine-readable instruction on what to do next. The orchestrator puts it into the agent context alongside the refusal text. Results with status `fail` always carry it; other statuses leave it empty.

| Value | Meaning |
|---|---|
| `revert-paths` | Drop the changes to the listed files |
| `reduce-scope` | Narrow the diff, move the rest into a separate PR |
| `add-test` | Add a test that covers the fix |
| `fix-test` | A test exists but misses the fix: rewrite the test |
| `fix-code` | The test is honest and red on head: fix the sources |
| `human-required` | The agent has no legal move |

One `human-required` anywhere in the report yields exit code `5` regardless of the other gates. A gate must declare `remediation` statically, by failure type, rather than deriving it from diff contents: the value lands in the agent context and has to stay predictable.
---

## 6. Config

A `redfirst.toml` file at the repository root, read from base. **Optional.** Without the file the built-in defaults apply, and that is the normal mode for tiers 0 and 1 rather than a degradation.

### Built-in defaults

```toml
[budget]
max_files = 20
max_added_lines = 800
max_deleted_lines = 400

[paths]
# Blocking. An edit to any of these changes the rules the agent is judged by.
protected = [
  ".redfirst/**", "redfirst.toml", ".github/workflows/redfirst.yml",
  "CLAUDE.md", "AGENTS.md", ".mcp.json", ".claude/**", ".cursor/**",
]
# Report line only, no effect on the verdict.
guarded = [
  ".github/**",
  "Dockerfile*", "docker-compose*.yml",
  "**/migrations/**",
  "**/*.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum",
]

[tests]
patterns = [
  "**/*.test.*", "**/*.spec.*", "**/*_test.go", "**/test_*.py",
  "tests/**", "spec/**", "__tests__/**",
]
# Test surface: files that decide test outcomes
# without holding test cases themselves.
fixtures = [
  "**/__snapshots__/**", "**/*.snap", "**/*.golden",
  "**/testdata/**", "**/fixtures/**", "**/__mocks__/**",
  "**/conftest.py", "**/setupTests.*", "**/*.setup.ts", "**/*.setup.js",
  "jest.config.*", "vitest.config.*", "playwright.config.*",
]
immutability = "append-only"
fixtures_immutability = "warn"
require_new = false        # off in defaults, see new-test-present

[severity]
diff-budget = "warn"
```

The defaults serve tiers 0 and 1, meaning human PRs for the most part, and follow the same rule that keeps `new-test-present` switched off: a gate that fires on a normal human diff gets the tool deleted rather than the diff fixed. Three decisions follow.

`protected` in the defaults shrinks to what it exists for: files whose edits change the rules for future agents. Infrastructure, migrations and lockfiles move to `guarded`, which reaches the report without blocking. A human who bumped dependencies sees a line and merges; an agent that went into the migrations catches a reviewer's eye.

The budget rises to a size that fits an ordinary PR. The exact number depends on the repository, and you do not have to guess it: `redfirst audit --last 50 --per-pr --report csv` prints the distribution over your own history, and you set the budget at its upper quartile.

`immutability` defaults to `append-only` rather than `strict`. Strict mode forbids any edit to an existing test file, which forbids renaming a symbol covered by tests. The agent has no legal diff for such a task, retries do not help, and the report carries a refusal that a human overrides by hand anyway.

`redfirst init --config` generates the strict set for agent branches: `require_new = true`, `immutability = "cases"`, `fixtures_immutability = "strict"`, a narrow budget and `severity.diff-budget = "fail"`. The split matches `new-test-present`: defaults stay out of a human's way, the template judges the agent.

`redfirst explain` prints the resulting set so a human can see what applied.

### Full config

The example below configures a repository that judges agent branches. It runs stricter than the built-in defaults, which serve human PRs on tiers 0 and 1.

```toml
version = 1

# Numbers for agent branches. Built-in defaults are wider: 20 / 800 / 400.
[budget]
max_files = 8
max_added_lines = 300
max_deleted_lines = 120

[paths]
# Paths the diff has no right to touch. A violation blocks.
# Syntax: gitignore-compatible glob patterns.
protected = [
  ".redfirst/**",
  "redfirst.toml",
  ".github/workflows/redfirst.yml",
  "CLAUDE.md",
  "AGENTS.md",
  ".mcp.json",
  ".claude/**",
  "drizzle/migrations/**",
  "src/lib/server/auth/**",
  "src/lib/server/billing/**",
]
# Paths under observation. They reach the report, verdict unchanged.
guarded = [
  ".github/**",
  "Dockerfile*",
  "docker-compose*.yml",
  "Caddyfile",
  "**/*.lock",
]
# Optional: when set, the diff may touch ONLY these paths.
allowed = []

[tests]
patterns = ["**/*.test.ts", "**/*.spec.ts", "tests/**/*.ts"]
# Test surface: affects test outcomes, holds no test cases.
fixtures = ["**/__snapshots__/**", "**/*.snap", "**/fixtures/**", "vitest.config.*"]
# strict      - existing test files are untouchable
# append-only - you may add lines, you may not delete them
# cases       - edits allowed as long as no existing test case
#               disappeared or went red on head (needs hooks)
immutability = "cases"
# Mode for tests.fixtures. Same values plus warn.
# warn - a snapshot or golden edit only gets flagged in the report.
fixtures_immutability = "strict"
require_new = true
forbid_disabled = ["\\.skip\\(", "\\.only\\(", "\\bxit\\(", "\\bxdescribe\\("]
# Known unstable tests. Fully exempt from suite-green.
known_flaky = []

[red_green]
enabled = true
# How many times to run the probe. Every run must return the same result.
probe_runs = 3
# How to treat a new test that fails to compile on base
# red        - count as red (the test uses a new API, which is legitimate)
# weak-red   - count as red but flag it in the report
# fail       - count as a violation
compile_failure_on_base = "weak-red"

[suite]
# How many times to rerun failed tests in suite-green before classifying
retry_failed = 1

[suppression]
# Report warnings only, no effect on the verdict
enabled = true
patterns = [
  "catch\\s*\\(\\s*\\w*\\s*\\)\\s*\\{\\s*\\}",
  "@ts-ignore",
  "@ts-expect-error",
  "eslint-disable",
  ":\\s*any\\b",
]

[severity]
# fail - a violation blocks, warn - report line only.
# Lowering red-green or suite-green to warn is forbidden: exit code 3.
protected-paths  = "fail"
diff-budget      = "fail"
test-immutability = "fail"
new-test-present = "fail"
no-disabled-tests = "fail"

[gates]
# Selective disabling. An empty list means all enabled.
disabled = ["blast-radius"]

[scopes]
# Whether to allow diffs that cross scope boundaries.
# For autonomous agents, false is the recommendation.
allow_cross_scope = true

# Scopes. v1 parses and validates them, but a non-empty list yields exit code 3.
# [[scope]]
# name = "payments"
# match = ["packages/payments/**"]
# hooks_dir = ".redfirst/payments"
# budget = { max_files = 3, max_added_lines = 80 }
```

### Validation

| Situation | Behaviour |
|---|---|
| No file | Built-in defaults, normal mode |
| File exists, TOML invalid | Exit code `3` |
| File exists, unknown key | Exit code `3` |
| File exists, `version != 1` | Exit code `3` |
| File exists, non-empty `[[scope]]` | Exit code `3`, `scopes are not implemented yet` |
| `severity` lowers `red-green` or `suite-green` | Exit code `3` |
| `immutability = "cases"` with no hooks | Downgrade to `append-only`, report line |
| The same path in both `protected` and `guarded` | Exit code `3` |

Ignoring a typo in silence is off the table: a config with `protected_paths` instead of `protected` must not turn quietly into an empty denylist. That is why a missing file and a broken file take separate paths, the first being a deliberate user choice and the second an error.

Downgrading `immutability` without hooks moves toward strictness: `append-only` forbids more than `cases` does. The reverse downgrade is forbidden everywhere.

### How your config joins the defaults

The unit of replacement is the **key**, not the section. A key you set displaces the default value whole, a key you leave out keeps its default. The section as a unit takes no part in anything.

```toml
[paths]
allowed = ["src/**"]
```

Here `protected` stays at its default. You get an empty denylist only by writing `protected = []`.

The difference from section-level replacement goes beyond cosmetics. Under section-level replacement the snippet above zeroes out all of `protected` in silence, which opens the exact hole that validation closes by treating an unknown key as an error. A typo gets caught while correct syntax with an unintended effect walks through.

Lists never merge: a `protected` you set replaces the default one instead of extending it. The reason matches our rejection of config inheritance (section 13). If you want the defaults plus your own entries, write both lists out, and `redfirst explain` will show the result.

### Scopes in a monorepo

One config at the root. No inheritance. Differences between packages live in `[[scope]]` sections inside that same file.

Resolution rules:

1. A file belongs to the **first** matching scope. Order in the file matters.
2. A file that matches no scope falls under the root rules.
3. A diff spanning several scopes falls under the **strictest** set: each numeric limit takes the minimum across the scopes touched, forbidden paths unite, and a gate counts as disabled only when disabled in every scope touched.
4. Hooks run for each scope touched. `red-green` and `suite-green` must pass in every one.

v1 freezes the shape and defers the implementation: a non-empty scope list returns exit code `3`. That way adding scopes in v2 will not break anyone.

---

## 7. Hook contract

The scripts live in `.redfirst/` and get read **from the base ref**. `redfirst` copies them into a temporary directory before running them. **The directory is optional**: without it, `red-green` and `suite-green` come back as `unavailable`.

| Script | Required for tier 2 | Input | Output |
|---|---|---|---|
| `env-up.sh` | yes | `$REDFIRST_RUN_ID` | Services up, exit code 0 |
| `env-reset.sh` | no | `$REDFIRST_RUN_ID` | Data reset to a clean state, schema untouched |
| `test.sh` | yes | `$REDFIRST_WORKDIR`, `$REDFIRST_FILTER`, `$REDFIRST_RESULTS`, `$REDFIRST_RUN_ID`, `$REDFIRST_PHASE`, `$REDFIRST_PROBE_INDEX` | Working copy prepared plus the run, exit code 0 when green |
| `env-down.sh` | yes | `$REDFIRST_RUN_ID` | Services down, exit code 0 |
| `env.sh` | no | none | Exports variables ahead of the rest |

A project with no external services leaves `env-up.sh` and `env-down.sh` as empty stubs and fits the whole harness into one line of `test.sh`. The `node-unit` preset in `redfirst init` does exactly that.

### Two layers of environment

The split matters and goes beyond cosmetics.

**The service layer** covers the database, cache, broker and external API mocks. Expensive to bring up, safe to reuse between the base and head runs as long as data gets reset. The database schema on base and head match by construction: the migrations sit in protected paths, so the agent did not change them.

**The working copy layer** covers the worktree, dependencies and build artifacts. It gets recreated for every **phase**. A compiled `dist` from the base phase that survives into the head phase turns a nonexistent fix green.

That is why dependency installation and the build live in `test.sh` rather than in `env-up.sh`.

### Phase and run

This distinction decides whether the check costs four minutes or twenty-five.

**A phase** is one half of `red-green`: the inventory and probes on base, then the probes on head. Plus a separate `suite` phase for `suite-green`. The source tree differs between phases, so the working copy gets destroyed and recreated, dependencies get installed, and the code gets built again.

**A run** is one iteration of `test.sh` inside a phase. Inside a phase the tree is identical byte for byte, a rebuild would change nothing, so all `probe_runs` iterations share one working copy. Otherwise `probe_runs = 3` would mean six dependency installs and six builds for a single PR.

`$REDFIRST_PHASE` takes `base`, `head` or `suite`. `$REDFIRST_PROBE_INDEX` numbers the runs inside a phase starting at one. A hook uses them to install dependencies and build only on the first run of a phase:

```bash
if [ "$REDFIRST_PROBE_INDEX" = "1" ]; then
  pnpm install --frozen-lockfile
  pnpm build
fi
pnpm vitest run --no-cache --reporter=junit --outputFile="$REDFIRST_RESULTS" $REDFIRST_FILTER
```

Repeating a run in the same copy imposes a requirement on the runner: it has to execute the tests again instead of returning a cached verdict from the previous run. For Go that means `-count=1`, otherwise three probes collapse into one and `probe_runs` stops meaning anything. `redfirst doctor` has to catch this by comparing the duration of the first and second run.

### Environment modes

The presence of `.redfirst/env-reset.sh` picks the mode, not a config flag.

| Mode | Condition | Behaviour |
|---|---|---|
| `reused` | `env-reset.sh` present | `env-up.sh` once, `env-reset.sh` between runs, `env-down.sh` at the end |
| `fresh` | No `env-reset.sh`, or `--fresh-env` given | Full up/down cycle per run |

An option to reuse without resetting does not exist.

Run order is fixed: base first, then head. Always.

The mode goes into the first line of the report. Someone digging into a strange refusal has to see `env_mode: reused` right away instead of guessing.

### Requirements for `test.sh`

- When `$REDFIRST_FILTER` is non-empty, run **only** those files. Projects without filtering work in form, but the probes in `red-green` will run the whole suite three times. `redfirst doctor` has to detect this and warn.
- Write JUnit XML or TAP to `$REDFIRST_RESULTS`. Content decides the format, not the extension.
- Results have to be **per case and named**: JUnit `<testcase classname name>`, TAP the description after the number. Case names stay stable between runs. Without them `red-green` cannot tell an added case from an existing one inside the same file and cannot judge edits to existing test files.
- Do not return a cached result from a previous run.
- Have no side effects outside the worktree and the running services.

Per-case names are the one requirement `redfirst` cannot work around on its own. Parsing sources would give the same knowledge and would demand a parser per language, which section 1 forbids. Case names arrive from someone else's test runner, which keeps them language independent.

### Isolation

Every run gets a unique prefix in `$REDFIRST_RUN_ID`. Hooks have to use it in container, network and port names, or parallel checks of two branches will fight over resources. See the reference hooks in the repository for an example.
---

## 8. Gates

Every gate has a stable ID, switches off in the config, and takes its own line in the report.

### Statuses

| Status | Meaning | Effect on the verdict |
|---|---|---|
| `pass` | Checked, no violations | none |
| `fail` | Violation | Exit code `1` |
| `warn` | Note for the reviewer | none |
| `skip` | Did not run: disabled in config, or cut off by short-circuit | none |
| `unavailable` | Cannot run: no hooks or no data | none |

`skip` and `unavailable` stay separate by design. The first reflects a user choice, the second a missing capability. In the report, `unavailable` has to carry its reason: `no hooks`, `no test files in diff`, `no stack trace provided`.

Exit code `0` with an `unavailable` in the report is a valid result. That describes tier 1.

### Capabilities

The runner computes `unavailable`, not the gate. A gate declares its requirements and never learns whether they hold.

| Capability | Where it comes from | Who needs it |
|---|---|---|
| `CapDiff` | Always | everyone |
| `CapHooks` | Base has `.redfirst/` and `--no-hooks` was not passed | `suite-green`, `red-green` |
| `CapTestFiles` | The diff contains files under `tests.patterns` or `tests.fixtures` | `red-green` |
| `CapCaseNames` | The inventory run returned named cases | `red-green`, `test-immutability` in `cases` mode |
| `CapStackTrace` | The caller passed a stack trace | `blast-radius` |

Three of the five capabilities depend on the run input rather than the environment, so the runner computes the set from `(Env, Diff, HarnessProbe)` instead of the environment alone. If `Requires()` looked at the environment only, a gate would have to check for test files in the diff itself, which is the exact check the capability model exists to take out of gates.

`CapCaseNames` arrives late: the first harness run produces it. The runner performs the inventory run as part of bringing up the environment, ahead of any tier 2 gate, so the capability set is complete by the time those gates start. A gate never runs with an unknown capability.

### Order

The order is mandatory and works as a short-circuit: static gates cost milliseconds and cut the run off before any work with the environment.

```
tier 1 · static, milliseconds, no environment
    1. protected-paths
    2. diff-budget
    3. test-immutability
    4. new-test-present
    5. no-disabled-tests
    6. suppression-scan

tier 2 · needs hooks
    7. suite-green
    8. red-green
```

`red-green` comes eighth rather than seventh: with a red suite on head, running probes buys nothing.

### protected-paths

No file in the diff matches `paths.protected`. When `paths.allowed` is non-empty, every file has to match it.

The gate checks all statuses: addition, modification, deletion, rename. A rename counts under both paths.

Files from `paths.guarded` produce a line in the "Needs reviewer attention" block and leave the verdict alone. The same path in both lists is a configuration error, exit code `3`, rather than a silent choice in someone's favour.

`remediation`: `revert-paths`.

### test-immutability

The gate answers two questions: did the agent remove existing insurance, and did the agent bend the test surface toward its own result.

**Test files** (`tests.patterns`), mode from `tests.immutability`:

| Mode | Rule | Tier |
|---|---|---|
| `strict` | An existing test file is neither modified nor deleted | 1 |
| `append-only` | Modifications allowed when the file's diff has zero deleted lines | 1 |
| `cases` | No previously existing test case disappeared or went red on head | 2 |

Mode `cases` exists because the first two forbid legitimate refactoring. Renaming a function covered by tests requires edits to test files: `strict` rejects that outright, `append-only` rejects it on the deleted lines holding the old name. The agent has no legal diff in either mode, a retry does not help, and a human receives a refusal they will override by hand.

`cases` looks at outcomes instead of lines: the set of case names from the inventory run on base has to sit entirely inside the set of names on head, and every carried-over case has to be green on head. Renaming a symbol passes; deleting a test or hollowing it out into a stub does not. `cases` misses a weakened assertion inside an existing case, and `append-only` catches that one on the deleted lines. The two modes go blind in different places, which is why `strict` and `append-only` stay in the spec.

Mode `cases` needs hooks and per-case names. Without hooks the mode downgrades to `append-only` with a line in the report. The downgrade moves toward strictness, so invariant 2 holds.

**Test surface** (`tests.fixtures`), mode from `tests.fixtures_immutability`, same values plus `warn`.

Snapshots, golden files, `testdata`, mocks and test runner configs decide test outcomes without holding test cases. Editing a snapshot to match new output is the cheapest way to turn a suite green while fixing nothing, and without a separate list it walks past every gate: such a file falls outside `tests.patterns`, sits nowhere in `protected`, and holds no `.skip` to find. The same goes for `conftest.py`, where a failing dependency gets mocked out, and for `vitest.config.ts`, where the list of included tests changes.

Defaults use `warn`: a human who updated a snapshot alongside a legitimate layout change gets a report line instead of a red cross. The `redfirst init --config` template sets `strict`, because on an agent branch a snapshot update almost always means bending the result.

`remediation`: `fix-code` under `cases`, `revert-paths` in the other modes.

### diff-budget

The number of changed files and lines stays within `[budget]`.

We count with `git diff --numstat <merge-base> <head>`, where `merge-base` matches the one in `red-green`. Two-dot `git diff <base> <head>` is off limits: it shows whatever landed on base while the agent worked as a revert, and a long-lived branch takes a refusal for someone else's commits.

The count excludes files marked `linguist-generated` in `.gitattributes` and files matching `tests.patterns`. Tests stay out of the budget because `new-test-present` demands them, and counting them against the limit would penalise the agent for satisfying a neighbouring gate. The report prints test lines as a separate number.

The gate carries `severity = "warn"` in the defaults. A swollen diff on a human PR calls for a closer look, not a blocked merge. The agent-branch template sets `fail`.

`remediation`: `reduce-scope`.

### new-test-present

The diff holds either a new file matching `tests.patterns` or added lines in an existing file that matches.

The unit is not the file, by design. A Go regression test goes into the existing `total_test.go`, a pytest one into the existing `test_order.py`. Demanding a new file would mean one new test file per fix and would break the convention of three languages out of the five in the preset list.

The gate is static and checks the fact of addition only. `red-green` does the substantive check, and it works by case rather than by file.

**Off in the defaults** (`require_new = false`). On tier 1 the gate faces human PRs, where refactors and config edits are legitimate diffs with no new test, and a `new-test-present` enabled by default would make the tool unbearable on day one. Whoever judges agent branches switches it on, and the `redfirst init --config` template sets it to `true` with a comment.

### red-green

The most valuable gate and the hardest part of the implementation. It checks that the added tests catch the bug the fix was written for. It needs hooks; without them the status is `unavailable`.

The unit of the check is **a test case, not a file**. File granularity leaves an open channel: the agent appends a fake case to an existing test file, the diff holds zero new files, the gate has nothing to probe, and the verdict comes out zero. Under `immutability = "append-only"` such a diff is legitimate, and `require_new` is off in the defaults, so the channel stays open in the normal configuration.

A run computes the set of added cases, not a source parse: `redfirst` knows no languages, and someone else's test runner supplies the case names.

Algorithm:

1. Compute `base_commit = git merge-base <base> <head>`.
2. Collect `F`, every file in the diff matching `tests.patterns` or `tests.fixtures`. When `F` is empty the gate reports `unavailable` with the reason `diff contains no test files`.
3. Create worktree `W_base` at `base_commit`.
4. **Inventory.** When `F` holds files that exist on base, run `test.sh` filtered to them **once, with nothing overlaid**. Take `C_before`, the set of case names. For a diff of new files only, skip this step and leave `C_before` empty.
5. **Overlay.** Put the head versions of `F` onto `W_base` whole. Nothing else. The fix does not travel.
6. Run `test.sh` filtered to `F` **`probe_runs` times**. Take `C_probe` and the status of every case in every run.
7. `C_new = C_probe \ C_before`. Expectation: `C_new` is non-empty and **every** case in it is red across all runs.
8. Recreate the working copy as `W_head` at `<head>`, then reset or rebuild the services according to the mode.
9. Run `test.sh` with the same filter **`probe_runs` times**.
10. Expectation: **every case in `C_new` is green across all runs**.

Outcomes of step 7, and separating them is mandatory:

| Base probe result | Code | Reason in the report | `remediation` |
|---|---|---|---|
| Every case in `C_new` red across all runs | on to step 8 | none | none |
| `C_new` is empty | `1` | `test files changed but no new test case appeared` | `add-test` |
| At least one case in `C_new` green across all runs | `1` | `test is green on base, it does not cover the fix` | `fix-test` |
| At least one case in `C_new` gave a mixed result | `1` | `flaky test, base probe is not deterministic` | `fix-test` |

Demanding the same result across all runs, rather than the bare fact of a failure, closes the one silent-pass channel in the whole system. Section 13 has the details.

The inventory run happens only where the diff touches existing test files. A typical fix with one new file skips it, so the gate costs what it always cost.

### When case names are missing

Not every harness returns per-case results. Behaviour depends on what the diff holds.

| Contents of `F` | Behaviour without case names |
|---|---|
| New files only | Probe by file. Equivalent to per-case: a new file holds no existing cases |
| Modified existing files present | Exit code `5`, reason `cannot verify added cases: test results lack per-case names`, `remediation: human-required` |

The second row gives exit code `5` rather than `1` because the agent has no legal move: the harness on base decides the case names and the agent cannot reach it. Returning `1` would burn the retry budget and escalate a valid task to a human. Returning `0` would reopen the channel the gate moved to cases in order to close.

`redfirst doctor` has to check this in advance and print the specific command for the specific runner instead of the bare fact of absence.

### Compile failure on base

A new test may import a function that base does not have yet. The run then dies before the assertions, no case names appear, and `C_new` cannot be computed.

`red_green.compile_failure_on_base` governs the behaviour, default `weak-red`: count the whole file set as red, substitute the set of names from the head run for `C_new`, and flag in the report that the red came from somewhere other than an assertion. In that mode the check degenerates to file level, and the report has to say so, because "the code does not compile without the fix" claims less than "the test catches the bug".

### suite-green

A full unfiltered `test.sh` run on `W_head`. Once, not `probe_runs` times. Needs hooks; without them the status is `unavailable`.

Failed tests get rerun `suite.retry_failed` times, then classified by origin:

| Test touched by the diff | Went green on retry | Outcome |
|---|---|---|
| Yes | Either way | Violation, exit code `1` |
| No | Yes | Warning in the report, verdict unchanged |
| No | No | Run the suite on base. Red there too → exit code `2`. Green → exit code `1` |

The agent answers for what it wrote and does not answer for flakes that sat in the repository before it arrived. Without that split the first foreign flake gets charged to the agent, the orchestrator spends three attempts on it, and a human receives a fake problem.

Tests listed in `tests.known_flaky` fall outside the gate entirely. The list comes from base, so the agent cannot extend it.

`remediation`: `fix-code`.

Running the suite on base costs a lot and happens on a rare path only: `suite-green` red, the failed tests untouched by the diff, the retry no help. We pay for separating `1` from `2` at the moment the separation matters.

### no-disabled-tests

No added line in a test file matches `tests.forbid_disabled`. This closes the trick of writing a test and marking it `.skip` in the same breath.

The gate checks added lines in every file under `tests.patterns`, not only in new ones: under `append-only` and `cases` the remaining gates permit appending `.skip(` to an existing file.

`remediation`: `revert-paths`.

### suppression-scan (warning only)

Searches added lines for the `[suppression]` patterns. It leaves the verdict alone and fills a **Needs reviewer attention** block in the report. An empty catch or a `@ts-ignore` inside an autofix almost always marks a silenced symptom.

### blast-radius (off by default in v1)

Changed files have to be reachable from the stack trace of the original error. This needs a stack trace passed into `redfirst` and a language-specific import resolver. Design the interface, defer the implementation to v2.
---

## 9. Report

### Text, tier 2

```
redfirst v0.4.1 · verify · tier=2 · env_mode=reused · probe_runs=3
base f3c9a11  head 8b2e740  diff 4 files (+87 -12), tests 1 file (+31)

PASS  protected-paths
PASS  diff-budget            4/20 files, +87/800, -12/400
PASS  test-immutability      cases · 0 removed, 0 regressed
PASS  new-test-present       src/lib/order/total.test.ts
PASS  no-disabled-tests
PASS  suite-green            412 passed, 1 flaky (untouched)
FAIL  red-green              flaky test, base probe is not deterministic
      case "promo item does not break the total"  base runs: fail, pass, fail
      remediation: fix-test

WARN  suite-green            tests/integration/webhook.spec.ts passed on retry
WARN  guarded-paths          pnpm-lock.yaml

REVIEW NEEDED
  src/lib/order/total.ts:42   empty catch block added

VERDICT: FAIL (1 gate)  exit=1
```

The `red-green` failure line names the case, not the file. In a file holding twenty cases, "the file went red" tells a reviewer nothing.

### Text, tier 1

```
redfirst v0.4.1 · verify · tier=1 · config=defaults
base f3c9a11  head 8b2e740  diff 4 files (+87 -12)

PASS  protected-paths
WARN  diff-budget            24/20 files, +910/800, -12/400
FAIL  test-immutability      append-only · src/lib/order/total.test.ts was deleted
SKIP  new-test-present       disabled by default
PASS  no-disabled-tests

N/A   suite-green            no hooks, add .redfirst/ to enable
N/A   red-green              no hooks, add .redfirst/ to enable

VERDICT: FAIL (1 gate)  exit=1
```

An `N/A` line has to carry the hint for removing it. This is the one place in the interface where advertising the next tier fits.

Wording rule: a failure line explains **what is wrong**, not **what got checked**. `test is green on base` helps, `red-green check failed` does not.

### JSON

The schema is versioned and lives in `schema/report.v1.json`. The orchestrator parses it, so breaking it without a version bump is off limits.

```json
{
  "schema": 1,
  "redfirst_version": "0.4.1",
  "tier": 2,
  "config_source": "base:redfirst.toml",
  "base": "f3c9a11", "head": "8b2e740",
  "env_mode": "reused",
  "probe_runs": 3,
  "diff_digest": "sha256:9c1f…",
  "base_probe_key": "sha256:4ade…",
  "verdict": "fail",
  "exit_code": 1,
  "diff": { "files": 4, "added": 87, "deleted": 12 },
  "gates": [
    { "id": "red-green", "status": "fail",
      "message": "flaky test, base probe is not deterministic",
      "remediation": "fix-test",
      "evidence": [ { "file": "src/lib/order/total.test.ts",
                      "case": "promo item does not break the total",
                      "base_runs": ["fail", "pass", "fail"],
                      "head_runs": [] } ] },
    { "id": "suite-green", "status": "unavailable", "reason": "no hooks" }
  ],
  "warnings": [
    { "kind": "flaky", "file": "tests/integration/webhook.spec.ts",
      "detail": "passed on retry, not touched by diff" },
    { "kind": "suppression", "file": "src/lib/order/total.ts",
      "line": 42, "pattern": "empty catch" }
  ],
  "timings": { "total_s": 214, "env_up_s": 48, "base_probe_s": 61,
               "head_probe_s": 58, "suite_s": 44, "base_suite_s": 0 }
}
```

`config_source` takes the value `base:<path>` or `defaults`. From it the orchestrator sees whether the diff got judged by project rules or by built-in ones.

The core computes and publishes `diff_digest` and `base_probe_key` without using either. The orchestrator needs the first to deduplicate identical patches between attempts. The second exists so that three months of logs can answer whether a cache would have paid for itself. Section 13 carries the reasoning.

---

## 10. Technical requirements

- Go, latest stable release, `CGO_ENABLED=0`, static linking.
- Dependencies: stdlib plus a TOML parser and a glob matcher. Every new dependency goes through an issue first. The reason is not aesthetics: people install this binary into their own CI, and the audit has to stay feasible.
- Git through the system `git` binary, not through go-git. The behaviour has to match what a developer sees by hand.
- No persistent state between runs. A test proves it: two consecutive `verify` runs on the same input produce byte-identical JSON apart from the timings.
- Cross-compilation: `linux/amd64`, `linux/arm64`, `darwin/arm64`.
- Target binary size under 15 MB.
- Reproducible builds, SHA256 published in the GitHub Release.
- Zero network calls at runtime, `init` included. A test proves it.
- Every temporary directory gets removed, including on the panic path.
- Logs and error messages in English. Code and comments in English.

### Version policy

Always the latest stable release, never an experimental one.

- Go: the newest released version, both in `go.mod` and in the CI toolchain. No beta, no release candidate, no `gotip`.
- Dependencies: the newest stable release of each. No prerelease tags, no pinning to a commit off `main`, no `v0.x` package where a stable alternative exists.
- Tools in `make check` (`gofumpt`, `golangci-lint`, `deadcode`): newest stable, one version pinned for the whole team so a local run and CI agree.
- Renovate or Dependabot opens the update PRs, and a human reviews them. Nothing auto-merges.
- An upgrade lands as its own commit, separate from feature work, so a regression traces to one change.
- Exception path: staying on an older version needs a comment in `go.mod` or in the CI config naming the reason and the condition for moving on. Silence means "we forgot", and nobody can tell the two apart six months later.

Performance budgets:

| Item | Budget | How it gets checked |
|---|---|---|
| `verify --no-hooks` on a 1000-file diff | 300 ms, 64 MB RSS | CI benchmark with a regression threshold |
| Process start to the first gate | 50 ms | Same |
| Core overhead on top of total hook time on tier 2 | 5% | End-to-end benchmark with a fake harness |

The third row matters more than the first two. Tier 1 fits inside 300 ms in Go without effort, while the hooks decide the cost of tier 2, and the core's job there is to add nothing on top. Cost model of a run:

```
env_up + worktree × 2 + (1 + probe_runs) × filtered_run
       + probe_runs × filtered_run + suite [+ base_suite]
```

The `1` in front of `probe_runs` in the first bracket is the inventory run, and a diff with no modified test files does not pay it. `worktree × 2` covers dependency installation and the build, once per phase. The end-to-end benchmark measures that sum rather than the time spent on diff arithmetic.

---

## 11. Testing the core

Fixture repositories live in `testdata/fixtures/`, each a small git repository with branches prepared in advance:

| Fixture | What it checks |
|---|---|
| `clean-fix` | Honest scenario, every gate green |
| `zero-config` | No config → defaults, not exit code `3` |
| `no-hooks` | No `.redfirst/` → `red-green` and `suite-green` go `unavailable`, verdict stays valid |
| `touched-workflow` | `protected-paths` |
| `touched-claude-md` | `protected-paths` on agent instructions |
| `deleted-test` | `test-immutability` strict |
| `edited-test-append` | `test-immutability` append-only |
| `oversized-diff` | `diff-budget` |
| `no-new-test` | `new-test-present` with `require_new = true` |
| `fake-test` | `red-green`, test green on base |
| `fake-case-in-existing-file` | `red-green`, fake case appended to an existing file, `C_new` comes from the inventory |
| `no-new-case` | `red-green`, test file modified, not one new case |
| `no-case-names` | `red-green`, harness without per-case names plus a modified file → exit code `5` |
| `renamed-symbol` | `test-immutability` in `cases` mode: test edits during a refactor pass |
| `updated-snapshot` | `tests.fixtures`, snapshot bent to match wrong output |
| `flaky-new-test` | `red-green`, mixed probe result |
| `test-needs-new-api` | `red-green`, compile failure on base |
| `skipped-test` | `no-disabled-tests` |
| `empty-catch` | `suppression-scan` |
| `flaky-untouched` | `suite-green`, a foreign flake is not the agent's fault |
| `broken-base-suite` | `suite-green`, suite red on base too → exit code `2` |
| `reused-env` | Mode `reused`, no state leaking between runs |
| `broken-harness` | Exit code `2`, not `1` |
| `bad-config` | Exit code `3` |
| `partial-config` | A given key replaces its default, a neighbouring key in the section keeps its own |
| `guarded-only` | Only `guarded` paths in the diff → verdict `0` with a warning |
| `audit-squash-history` | `redfirst audit` on a squash-merge repository, mode and caveat in the header |
| `scoped-config` | `[[scope]]` in v1 yields exit code `3` with a readable message |
| `audit-history` | `redfirst audit --last N` on a repository with known history |

Golden tests compare the whole JSON report, refresh through the `-update` flag, and never get edited by hand. Fixture hooks are bash and launch fake test runners with no docker: a full run of the core suite has to fit inside a minute. A counter in a file simulates a flake.

### A test as an acceptance criterion

A green suite does not mean done. Three requirements beyond green:

1. **A detector test per gate.** A test has to exist that fails when you delete the gate. A test that stays green with and without the gate checks nothing and gets deleted. This is the same condition `redfirst` imposes on other people's code (`red-green`), turned on itself.
2. **The test name carries the requirement.** `TestProtectedPaths_RejectsRename`, not `TestProtectedPaths_Works`. Reading the list of names should tell you what is covered.
3. **Every fixture in the table above appears in at least one test.** A fixture without a test is an open requirement from the spec.

Mandatory boundaries for every gate, on top of the happy path: rename, deletion, binary file, empty diff, diff with no test files.

### Go specifics

- Isolation through `t.TempDir()` only. Fixture repositories get assembled in setup through `internal/testkit` rather than committed: a nested `.git` inside a git repository does not work.
- Table tests drive the S1 and S2 gates from `(Diff, Config)` with no fixture repository. Fixtures matter only where git or hooks take part.
- Every table case gets a name through `t.Run`, so a failure points at one behaviour.
- The whole suite runs with `-race`. A race in a tool that brings up containers and parallel probes is a blocking defect.
- Coverage of 80% per package is a floor, not a goal. Uncovered code gets deleted rather than wrapped in a test for the percentage.
- Benchmarks for the hot path (`verify --no-hooks` on 1000 files) sit next to the tests and run in CI with a regression threshold.
---

## 12. Development stages

The skeleton gets built whole first, then the slices run **one at a time, in order**. The order puts **a shippable artifact early**: after S3 a useful tier 0 tool exists, after S5 a tier 1 one.

A mandatory skeleton revision point sits between slices (see below). It exists because the shape of `Input` and `Result` after two real gates will almost certainly differ from the one drawn in advance, and admitting that as a slot in the plan beats smearing it across the slices.

The package layout below sets the **context boundary**: a session working on a slice reads its own packages plus the skeleton, not the whole repository. A slice does not edit packages assigned to other slices; needing an edit there means stop and ask, not patch in passing.

### Package layout

```
cmd/redfirst/           entry point                                       skeleton
internal/domain/        Diff, Config, Result, Report                      skeleton
internal/gitx/          git adapter                                       skeleton
internal/config/        defaults, loading from base                       skeleton
internal/runner/        registry, capabilities, order                     skeleton
internal/report/        text and json rendering                           skeleton
internal/testkit/       fixture builder, golden helper                    skeleton
internal/gates/paths/   protected-paths, diff-budget                      S1
internal/gates/tests/   test-immutability, new-test-present,               S2
                        no-disabled-tests, suppression-scan
internal/audit/         history walk, summary                             S3
internal/harness/       hooks, worktree, environment modes                S4
internal/cliux/         init, doctor, explain                             S5
internal/gates/exec/    suite-green, red-green                            S6
```

### S0 · Skeleton (sequential, before everything else)

The skeleton has to be finished completely: five sessions build on top of it and must not edit it.

| Item | Acceptance criterion |
|---|---|
| `go.mod` with **every** dependency pinned up front | No slice changes `go.mod`; CI checks the diff on `go.mod` and `go.sum` |
| Domain types: `FileChange` with status and counters, `Diff`, `Config`, `GateResult`, `Report`, `Verdict` | `TestDomain_FileChangeStatusRoundTrip`, a table over every git status including `R` and `C` |
| Capability model: `Capability`, `Gate.Requires()`, set computed from `(Env, Diff, HarnessProbe)`, `unavailable` in the runner | `TestRunner_GateWithoutCapabilityIsUnavailableWithReason`, `TestRunner_CapTestFilesDerivedFromDiff`; no gate checks for hooks or test files on its own |
| A `remediation` field in `GateResult`, mapping `human-required` to exit code `5` | `TestExitCode_HumanRequiredIsFiveNotOne` |
| Gate registry with **stubs for every declared ID** | `TestRegistry_AllDeclaredGatesRegistered` (the registry supplies the gate count instead of the test name hardcoding it); a slice replaces its own stub and leaves the registry alone |
| Execution order and short-circuit | `TestRunner_StopsBeforeEnvWhenStaticGateFails` |
| `internal/gitx`: ref resolution, merge-base, `--name-status`, `--numstat`, streaming reads | `TestGitx_ParsesRenameWithBothPaths`, `TestGitx_HandlesBinaryFile`, `TestGitx_EmptyDiff` |
| `internal/config`: built-in defaults, reading from base, per-key override, validation table | `TestConfig_MissingFileUsesDefaults` (not exit code `3`), `TestConfig_UnknownKeyIsExit3`, `TestConfig_KeyReplacesNeighbourKeyKeepsDefault`, `TestConfig_ListsNeverMerge` |
| `internal/report`: text and json, `tier`, `config_source`, schema v1 | Golden tests with the `-update` flag |
| Exit code mapping through sentinel errors | `TestExitCode_HarnessFailureIsTwoNotOne` |
| `internal/testkit`: git repository builder inside `t.TempDir()`, golden helper, fake test runner with a counter for simulating flakes | Fixtures get assembled programmatically; no nested `.git` inside the repository |

The skeleton counts as done when `redfirst verify --no-hooks` on the `clean-fix` fixture prints nine lines with status `unavailable` or `skip` and returns `0`.

`go.mod` freezes once the skeleton lands: every dependency gets pinned here. The point is not merge conflicts. The dependency set is a project-level decision and does not grow mid-slice. A slice that needs a new dependency stops and asks.

### The skeleton revision point

Runs after every slice, before the next one starts. Takes anywhere from zero to an hour and exists so that accumulated drift does not surface at S6.

| What to check | Sign that the skeleton needs an edit |
|---|---|
| The shape of `Input` | The slice reached for data around `Input` or duplicated its computation |
| The shape of `Result` and `evidence` | A gate could not express its refusal reason structurally and put it in a string |
| Capability model | A gate grew a check for hooks or for the tier |
| `internal/testkit` | The slice wrote its own fixture builder instead of using the shared one |
| Order and short-circuit | A gate depends on the result of another gate |

Any "yes" means a skeleton edit as its own commit, updating the slices already written, rather than a patch inside the new slice.

### Slices S1 to S5

The order is mandatory. Each slice ends with a green `make check`, closed acceptance criteria and a skeleton revision point.

| Slice | Contents | Fixtures | Acceptance criteria |
|---|---|---|---|
| **S1** · Path gates | protected-paths, diff-budget | `touched-workflow`, `touched-claude-md`, `oversized-diff` | A rename gets caught on both paths; `linguist-generated` stays out of the budget; globs compile once (benchmark on 1000 files) |
| **S2** · Test gates | test-immutability (`strict`, `append-only`, recognising `tests.fixtures`), new-test-present, no-disabled-tests, suppression-scan | `deleted-test`, `edited-test-append`, `no-new-test`, `skipped-test`, `empty-catch`, `updated-snapshot` | `append-only` passes a diff with zero deleted lines and fails with one deleted; a snapshot edit gives `warn` in the defaults and `fail` under `fixtures_immutability = "strict"`; `new-test-present` counts added lines in an existing file; `suppression-scan` leaves the verdict alone |
| **S3** · Audit | `redfirst audit`: history unit detection, walk, aggregation, text/json/csv | `audit-history`, `audit-squash-history` | The mode gets detected and printed in the header together with the accuracy caveat; the summary on a repository with known history matches exactly; the environment stays down even when hooks exist. **Shippable tier 0** |
| **S4** · Harness | Hook runner, worktree manager, phases and runs, `$REDFIRST_PHASE` and `$REDFIRST_PROBE_INDEX`, inventory run and `CapCaseNames`, `fresh` and `reused` modes, timeouts, teardown | `no-hooks`, `reused-env`, `broken-harness`, `no-case-names` | `env-down.sh` runs on panic, on timeout and on `SIGINT`; the process tree dies, not one process; the working copy gets created once per phase rather than per run, verified by a counter on dependency install calls; a missing `.redfirst/` yields `unavailable` rather than a refusal |
| **S5** · CLI and distribution | `redfirst init --ci/--config`, `explain --path`, `diff_digest`, `base_probe_key`, cross-compilation, release, GitHub Action | none | `init` is deterministic and works without network; the generated workflow pins the version and SHA256; the project's own CI starts running `redfirst` on itself in `warn` mode. **Shippable tier 1** |

The gates in S1 and S2 have to be pure functions of `(Diff, Config)`: no disk reads and no git calls. That condition makes them testable from a table with no fixture repository and keeps the 300 ms budget.

### S6 · Executing gates

| Slice | Contents | Fixtures | Acceptance criteria |
|---|---|---|---|
| **S6** · Executing gates | suite-green with retry and classification, red-green over cases with K-of-K probes, `test-immutability` in `cases` mode | `fake-test`, `fake-case-in-existing-file`, `no-new-case`, `no-case-names`, `renamed-symbol`, `flaky-new-test`, `test-needs-new-api`, `flaky-untouched`, `broken-base-suite` | A green case on base yields `1` with the reason `test is green on base`; a fake case in an existing file gets caught the same way as one in a new file; a modified test file without a single new case yields `1`; missing per-case names with a modified file yields `5` rather than `0` or `1`; mixed probes yield `1` with the reason `flaky test`; a foreign flake gives a warning while the agent's own gives a refusal; a suite red on base too yields `2` |

It builds on the harness from S4 and therefore follows it. The most expensive slice: budget as much for it as for S1 through S3 combined.

### Assembly

| Slice | Contents | Acceptance criteria |
|---|---|---|
| **S7** · Integration | End-to-end paths for codes `1`/`2`/`3`/`4`, `redfirst doctor`, hook presets `node-unit`, `node-postgres-docker`, `python-pytest`, `go`, `blank`, detection of an ignored `$REDFIRST_FILTER` | `doctor` names the specific cause and the specific command for reaching the next tier. **Shippable tier 2** |
| **S8** · Dogfooding and documentation | Dogfooding from S5 moves out of `warn` into blocking mode, `.redfirst/` hooks get added for our own Go repository; README, `docs/CONFIG.md`, `docs/HOOKS.md`, a rules block for someone else's `CLAUDE.md`, a reference `env-reset.sh` for Postgres | Our own CI green under our own rules; an outside reader adopts the tool without opening the sources |

v1 excludes blast-radius and `[[scope]]`.

---

## 13. Decisions and reasoning

This section exists so that nobody "optimises" these places six months from now without knowing why they look the way they do.

### Why probes in `red-green` demand an identical result rather than a bare failure

Flakes break the gate in two directions, and the two are not equivalent:

| Run | Expectation | A flake produces | Consequence |
|---|---|---|---|
| `suite-green`, suite on head | GREEN | RED | False accusation of the agent |
| `red-green`, test on head | GREEN | RED | False accusation of the agent |
| **`red-green`, test on base** | **RED** | **RED instead of GREEN** | **False acquittal** |

The third row is the one place in the whole system where randomness produces a pass. A fake test that sits green on base gets stamped as a real reproduction test on one random red and reaches a human with a green verdict.

Retries do not fix this. Rerunning a probe you expect to be red until it turns red bends the result to fit, a direct violation of invariant 2. So we demand determinism.

Side benefit: a non-deterministic regression test has no value on its own, and K-of-K cuts it off immediately.

We pay for this on a subset, not on the suite. A typical fix brings one or two new test files, and running them takes seconds.

### Why `red-green` judges cases rather than files

File granularity looked sufficient while `new-test-present` demanded a new file. Once the gate started counting added lines in an existing file, and `immutability` in the defaults became `append-only`, a diff with no new file at all became legitimate. A fake case appended to the end of an existing `order.test.ts` gave `red-green` nothing to probe, and the verdict came out zero.

The failure direction here matches the table above: a silent pass, forbidden by invariant 2. Patching it with "no new files means a violation" would restore the one-new-file-per-fix demand, the problem that prompted the rule change.

Case names come from someone else's runner results, not from a source parse. A parser per language would push knowledge of specific stacks into the core, which section 1 forbids, and would break on the first nonstandard way of declaring a test. The inventory run costs one extra execution of a filtered subset, and only where the diff touches existing test files.

Honesty costs us exit code `5` on harnesses without per-case names. The alternative, a quiet fall back to file-level probing, would restore the hole, so we rejected it.

### Why reusing the environment is safe, but only for services

Runs go base then head, so state can leak forward only. Data left over from the base run can turn the head run red. The expectation on head is green, so a leak produces **a refusal**, not a pass. The reverse direction cannot happen inside one invocation.

Service reuse therefore breaks toward safety: the worst outcome is an unfair `1`, which costs one retry.

Build artifacts work the other way. Compiled code from base that survives into the head run turns a nonexistent fix green, which is a false acquittal. So the working copy gets recreated always, with no options and no exceptions.

The database schema needs no reset: the migrations sit in protected paths, so base and head match by construction. This follows from `protected-paths` and is not an assumption. If a project moves migrations out of protected, reuse stops being correct, and `redfirst doctor` has to notice.

### Why the core has no cache and never will

A cache key has to include a hash of the **contents** of the new test files. Now look at when a hit becomes possible. If the attempt failed on `red-green`, the agent edits that very test on the next attempt, the contents differ, and the lookup misses. A hit happens only where the agent left the test byte for byte identical and rewrote the fix, meaning after a refusal from `diff-budget` or `suite-green`. Not the dominant scenario.

On top of that, any verification cache creates a failure direction that invariant 2 forbids: a stale "base was red" entry turns into a false acquittal.

Instead of a cache: short-circuit through gate order, `diff_digest` for deduplicating identical patches on the orchestrator side, and `base_probe_key`, which caches nothing but makes it possible to estimate a hypothetical hit rate from the logs. If that rate comes out above 30%, we build the cache **in the orchestrator**, which is already stateful and already has Postgres.

### Why the config is single, inheritance-free and optional

The config comes from base and marks the boundary of the agent's authority. One line in CODEOWNERS guards a centralised file. Forty files scattered across package directories are harder to guard, and slipping a quiet relaxation into one of them is easier.

Config inheritance is where ESLint and tsconfig trip, because the semantics of merging lists stay ambiguous. The rule "overlapping scopes take the strictest set" is predictable and stops an agent from widening its budget by touching a file in a scope with softer rules. For the same reason a list you set replaces the built-in one whole instead of extending it.

The unit of replacement is the key, not the section. Section-level replacement looked cheaper to describe and opened a hole: someone who declared a single `allowed` under `[paths]` zeroed out `protected` in silence and got an empty denylist from a formally valid config. Validation catches a typo in a key name and misses correct syntax with an unexpected effect, so the hole needed a rule rather than a check.

Making the config optional costs no security. The defaults are compiled into the binary, the workflow pins the binary version, and the workflow sits in protected paths. Swapping the defaults is exactly as impossible as swapping the config from base. Without that property tiers 0 and 1 do not exist, and without them the barrier to entry stays unreasonable.

A consequence of the "config from base only" rule: a new package added in the same PR does not bring its own scope with it. A separate PR with a config edit fixes that, and such a PR goes through a human anyway.

### Why the agent does not perform the install

Shipping a `CLAUDE.md` that says "download `redfirst` and set up CI" looks like a way to drop the barrier to entry to zero. We rejected it for three reasons, and the first suffices on its own.

The product claims the agent cannot influence what judges it. An agent-driven install refutes that claim at install time, and the first technical reader will notice.

Then the practice: the agent will write the workflow a little differently in every repository, skip the SHA pin in one place, get `protected` wrong in another. Supporting five hundred slightly different installations is impossible.

And the perception: "drop in a file and your agent will download a binary from the internet into your CI" describes a supply chain attack.

The idea of distributing through `CLAUDE.md` still holds, as long as you distribute rules instead of an install. Rules work without the binary, cut the share of violations on the first attempt and save tokens. And the sentence "this is a request the agent may ignore, which is why the binary exists" sells the thesis better than any landing page.

---

## 14. Open questions

1. **The value `probe_runs = 3`** is a guess. A hundred runs will show whether the third one catches anything the second missed.
2. **The numbers in `[budget]`** are also a guess, but one we can settle before release: `redfirst audit` over a dozen public repositories gives the distribution to set the default from.
3. **The default for `allow_cross_scope`** sits at `true` for the sake of humans. Splitting the defaults by diff authorship may make sense.
4. **Volume snapshots** as a third environment mode, if projects turn up where resetting data costs more than starting a container.
5. **The threshold for moving to a cache** is fixed at a 30% hit rate, but that figure came out of the air and needs checking against the real cost of a base run.
6. **Splitting budgets by task class.** One `[budget]` serves both a two-file fix and a twenty-file feature. The orchestrator passing a task class (`--intent fix|feature|chore`) with a class-to-limits mapping inside the config from base suggests itself: the thesis holds, because the orchestrator sets the class while the numbers stay out of the agent's reach. v1 leaves it out so that a mandatory flag does not arrive before the data from `audit` does.
7. **Detecting agent authorship.** Once it exists, `require_new` and `allow_cross_scope` can carry different defaults for human and agent branches. We leave it alone for now: the signal is unreliable, and a misclassification changes how strict the check gets.
