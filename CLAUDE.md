# CLAUDE.md, redfirst

A deterministic diff judge for CI. One static Go binary, no runtime and no network.

The project name states its thesis: no test is born green. A test that stayed green before the fix proves nothing.

Specification: `docs/redfirst-spec.md`. It is the source of truth. When this file and the spec disagree, the spec wins, and you raise the disagreement instead of resolving it yourself.

---

## 1. How to work

**Think before you code.** Before making changes, state the task, the goal and the expected result in your own words. Make no silent assumptions. When a requirement reads two ways, list both and ask. When you see a simpler path, say so. When you think the decision in the task is wrong, object before writing code.

**Keep it simple.** Add nothing nobody asked for. Do not abstract single-use code. Do not introduce config options the spec does not have. When fifty lines solve the task, do not write two hundred. Self-check: would a senior call this overengineered? If yes, rewrite it.

**Edit surgically.** Touch only what you must. Every changed line traces back to the task. Do not rewrite neighbouring functions while you are in the area. Clean up only the mess you made yourself: an import goes when your own change orphaned it.

**Work from an acceptance criterion.** Before the code, state a checkable condition for done, then loop until it holds. "It works" is not a criterion. "`TestProtectedPaths_RejectsRename` is red without the gate and green with it" is one.

**No backward compatibility unless asked.** The project has not shipped its first release. Leave no legacy code paths, write no shims, mark nothing `// deprecated`. Delete it.

**Comments explain why, not what.** `// compile globs once: the gate runs per diff file` earns its place. `// loop over files` gets deleted.

---

## 2. Resolving conflicts between the rules

Two of the rules above collide with project requirements. Resolve them this way and no other.

**"Write no defensive code" against guaranteed teardown.** The rule forbids *speculative* defence: checks against what cannot happen. It does not cancel invariants from the spec. Defensive code is allowed and required wherever the spec names an invariant: teardown on panic and timeout, distinguishing exit codes, cleaning temporary directories. Everywhere the spec stays silent, fail loudly and swallow nothing.

**"Do not abstract" against scalability.** The rule forbids abstraction for a *hypothetical* future. Abstraction is allowed where the spec already names two or more concrete implementations: `Gate` (nine of them), `Capability`, the report renderer (text and json), the hook presets. Everything else stays concrete until the spec brings a second case.

Needing a third abstraction the spec does not name is a signal to stop and ask, not to start designing.

---

## 3. What never enters this project

- Network calls at runtime, `init` included. A test proves it.
- Persistent state between runs: caches, databases, files in `$HOME`. The core is a pure function of `(repo, base, head)`.
- LLM calls and any heuristic built on them.
- Writes into the repository under judgement. Only stdout, stderr and the report file.
- Dependencies beyond a TOML parser and a glob matcher. A new dependency gets its own discussion, not a line added mid-task.
- Knowledge of specific languages and frameworks inside gate logic. The stack lives in config globs and in shell hooks. Test case names come from someone else's runner results, not from parsing sources.
- Logic that weakens a check under ambiguity. Every ambiguity resolves to a refusal.

---

## 4. Go style

**Gate identifiers.** A gate carries one word through the config, the report, the JSON, the filename and the test name: `protected-paths` → `protected_paths.go` → `TestProtectedPaths_RejectsRename`. Gates get no numeric codes, now or later: `disabled = ["blast-radius"]` reads without a dictionary, `disabled = ["G09"]` does not.

**Size.** Files up to 400 lines, functions up to 50, nesting up to three levels. Over the limit, cut along responsibility rather than splitting line count in half. The linters `funlen`, `lll` and `nestif` inside `make check` enforce the numbers, not a reviewer's eye.

**Interfaces.** Declare them at the consumer, not at the provider. Accept interfaces, return structs. An interface for one implementation does not get written until the spec brings a second.

**No package-level mutable state.** No `var cache = ...`, no singletons, no `init()` with side effects. This is not style: a test checks the statelessness invariant by comparing two runs byte for byte.

**Errors.** Wrap with `%w`. Sentinels (`errors.Is`) for branching by failure type, above all for separating exit codes `1`, `2`, `3` and `5`. Use `panic` only in an unreachable state, never as control flow.

**Context.** Anything that starts a process or waits takes `context.Context` as its first argument. Timeouts come from the config, not from constants in the code.

**Processes.** External commands go through `os/exec` with a process group, so the deadline kills the whole tree. Killing one process leaves containers alive, and that is a bug rather than a detail.

**Concurrency.** Parallel probes run through `errgroup` with a limit. Each run writes into its own directory. Tests run with `-race`; a race inside a CI tool is unacceptable.

**Idiom over OOP.** Composition and interfaces, not hierarchies. Struct embedding serves behaviour reuse, not inheritance.

---

## 5. Architecture: what you may not break

Four things hold the scalability. Changing any of them takes a discussion, not an edit in passing.

**The capability model.** A gate declares what it needs:

```go
type Capability int
const (
    CapDiff Capability = iota   // always available
    CapHooks                    // base has .redfirst/ and --no-hooks was not passed
    CapTestFiles                // diff holds files under tests.patterns/fixtures
    CapCaseNames                // harness returned named test cases
    CapStackTrace               // a stack trace was passed in
)

type Gate interface {
    ID() string
    Requires() []Capability
    Run(ctx context.Context, in Input) (Result, error)
}
```

The runner computes the capability set from `(Env, Diff, HarnessProbe)` rather than from the environment alone: three of the five capabilities depend on the run input. The inventory run produces `CapCaseNames`, and the runner performs it as part of bringing up the environment, ahead of any tier 2 gate.

The runner works out which capabilities exist and moves a gate to `unavailable` with a reason. No `if hooksExist` and no `if len(diff.TestFiles) == 0` inside gates, no tier checks in the code: tiers 0, 1 and 2 follow from this model instead of being separate modes. A new gate arrives as one package plus a requirements declaration, with no edit to the runner.

A gate's requirements stay fixed for the whole run. A gate that needs data which shows up only sometimes declares a capability instead of checking for the data inside `Run`.

**Gates know nothing about each other.** No references between gate packages. Shared data arrives through `Input`, gets computed lazily and cached for the duration of one run.

**The report schema carries a version.** `schema/report.v1.json` is the contract with the orchestrator. Adding a field needs no bump; renaming or removing one does.

**Exit codes and `remediation` are part of the contract, not an output detail.** `1` against `2` separates the agent's fault from a broken harness, and `5` marks a refusal with no legal move. Every failure type declares `remediation` statically, by type, instead of deriving it from diff contents: the value lands in the agent context and has to stay predictable.

---

## 6. Tests as acceptance criteria

A green suite is not a criterion for done. The criterion is a demonstration of the claimed behaviour.

**Every gate needs a detector test:** it fails when you delete the gate. A test that stays green with and without the gate checks nothing and gets deleted. This is the same requirement `redfirst` imposes on other people's code, turned on itself.

**The test name carries the requirement.** `TestProtectedPaths_RejectsRename`, not `TestProtectedPaths_Works`. Reading the list of names should tell you what got checked.

**Every fixture from the spec table has a test that references it.** A fixture without a test is an open requirement.

**Table-driven tests** are the norm for gates. Each case gets its own name through `t.Run`, so a failure points at one behaviour.

**Isolation.** `t.TempDir()` only. Fixture git repositories get assembled programmatically in setup rather than committed: a nested `.git` inside a repository does not work. The builder lives in `internal/testkit`.

**Golden files** update through the `-update` flag and never by hand.

**Boundaries, not the happy path.** Every gate needs these: rename, deletion, binary file, empty diff, diff with no tests. The happy path is one test in ten, not the other way round.

**Coverage** is a floor rather than a goal: 80% per package. Writing tests for the percentage is forbidden; delete the uncovered dead code instead.

The project's own CI runs `redfirst` on itself from the moment tier 1 ships (slice S5), in `warn` mode at first and blocking from S8.

---

## 7. Deterministic gates

All of these must be green before every commit:

| What we check | Command |
|---|---|
| Formatting | `gofumpt -l .`, output must be empty |
| Linters | `golangci-lint run ./...` |
| File size, function size, nesting | `funlen`, `lll`, `nestif` inside golangci-lint |
| Compiler and suspicious constructs | `go build ./...` and `go vet ./...` |
| Dead exports and unreachable functions | `deadcode ./...` |
| Dead unexported entities and fields | the `unused` linter inside golangci-lint |
| Races | `go test -race ./...` |
| Dependency hygiene | `go mod tidy`, then fail when `git diff` is non-empty |

`make check` collects all of it. Run it locally before committing, not "later in CI".

On `deadcode`: use `golang.org/x/tools/cmd/deadcode`, the official one, installed separately. golangci-lint carries a deprecated linter under the same name, which is a different thing and stays off.

The repository holds no dead code in any form. An unused function gets deleted rather than marked with a comment or kept "for later".

---

## 8. Versions of Go, libraries and tools

Always the latest stable release, never an experimental one.

- **Go**: the newest released version, both in `go.mod` and in the CI toolchain. No beta, no release candidate, no `gotip`.
- **Dependencies**: the newest stable release of each. No prerelease tags, no pinning to a commit off `main`, no `v0.x` package where a stable alternative exists.
- **Tools** (`gofumpt`, `golangci-lint`, `deadcode`, `goreleaser`): newest stable, one version pinned for the whole team so a local `make check` and CI agree.
- **Actions in the workflow**: pinned by commit SHA, and the SHA points at the newest release tag.

How upgrades happen:

- Renovate or Dependabot opens the update PRs. A human reviews them. Nothing auto-merges.
- An upgrade lands as its own commit, `chore(deps):`, separate from feature work, so a regression traces to one change.
- After a Go minor upgrade, run the full `make check` plus the benchmarks: the performance budgets in section 8 of the spec are checked against a threshold, and a compiler change can shift them.
- `go.mod` stays frozen for the duration of a slice (see section 10). Version bumps go between slices, never inside one.

Staying on an older version needs a comment in `go.mod` or in the CI config naming the reason and the condition for moving on. Silence reads as "we forgot", and six months later nobody can tell the two apart.

---

## 9. Performance

The tool runs inside someone else's CI on every PR. A slow gate gets switched off.

Budgets, checked by benchmarks:

- `verify --no-hooks` on a 1000-file diff: up to 300 ms, up to 64 MB RSS.
- Process start to the first gate: up to 50 ms.
- Core overhead on top of total hook time on tier 2: up to 5%.
- Binary size: up to 15 MB.

The third item matters more than the first two. Tier 1 fits inside 300 ms in Go without effort, while the minutes per PR come from tier 2, where the hooks do the work rather than the arithmetic over the diff. The core's job there is to avoid multiplying that work: the working copy gets created once per phase rather than per run, the inventory run happens only with modified test files, and all `probe_runs` iterations inside a phase share one copy. An end-to-end benchmark with a fake harness measures that sum.

Practical consequences:

- Globs and regexes compile once when the config loads, not inside the loop over files.
- `git` output gets read as a stream rather than through `ReadAll` into a string.
- File contents load lazily and only for the gates that need them.
- Short-circuit through gate order is mandatory: static gates cut the run off before any work with the environment.
- Benchmarks for the hot path live in the repository and run in CI with a regression threshold.

An optimisation without a benchmark does not get accepted: measure first, then edit.

---

## 10. Commits

Set the author once per repository:

```
git config user.name  "Tim Hartmann"
git config user.email "timhartmann7@proton.me"
```

Check `git log -1 --format='%an <%ae>'` after your first commit in a session. Wrong author means fix the config and redo the commit.

**Commit as you go, not in one lump at the end.** One commit, one logical unit: a gate, a refactor, the test kit, a fix. A whole slice does not go into one commit.

Format: conventional commits, subject up to 72 characters, imperative mood.

```
feat(gates): add protected-paths with rename handling
fix(harness): kill process group on timeout, not just the process
test(red-green): cover mixed probe results as flaky, not as green-on-base
refactor(report): split text renderer out of report.go
chore(deps): bump toml parser to latest stable
```

Write a body only when it explains why. Retelling the diff adds nothing.

Forbidden: `--no-verify`, `git add -A` without review, amending commits already pushed, committing with a red `make check`, trailers attributing work to an agent.

---

## 11. Working in slices

Development runs in slices, one at a time, in order. The spec lays out the slices and the package layout in its "Development stages" section.

- **A slice reads its own packages and the skeleton, not the whole repository.** The package layout sets the context boundary. Packages assigned to other slices stay out of context and out of the editor.
- **Needing an edit in someone else's package or in a shared type means stop and speak up.** That signals the skeleton needs changing, and such a change goes in as its own commit updating the slices already written, not as a patch inside the current one.
- **`go.mod` is frozen after the skeleton.** The dependency set is a project-level decision. A new dependency means stop and ask, not an edit in passing.
- **The gate registry stays untouched.** The skeleton declares all nine IDs as stubs. A slice replaces the file holding its own stub.
- **Each slice owns its own `testdata/` subdirectory.**
- **A slice closes with a skeleton revision point.** The spec holds the checklist of five signals. Walk it before the next slice starts, not at the end of the project.
- **A branch and a PR per slice**, so the history reads slice by slice.

---

## 12. Checklist before finishing a task

1. The acceptance criterion was stated before the code and holds now.
2. `make check` is green in full, `deadcode` and `-race` included.
3. Every new gate has a detector test that fails without the gate and declares `remediation` for each of its failure types.
4. Every new fixture appears in at least one test.
5. No file passed 400 lines and no function passed 50.
6. No dead code, no commented-out code, no TODO without an issue.
7. Not one line that fails to trace back to the task.
8. Commit author is Tim Hartmann, and the commits split along logical units.
9. No changes in other slices' packages and none in `go.mod`, or they went into a separate skeleton edit and you said so.
10. The skeleton revision point is done: five signals from the spec checked.
11. What stayed unfinished and why: said out loud, not left to guesswork.
