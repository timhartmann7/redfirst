# redfirst

A deterministic diff judge for CI. One static binary, no runtime and no network.

The name states the thesis: **no test is born green.** A test that stayed green
before the fix proves nothing, and a suite full of those is worse than no suite,
because it reads as coverage.

`redfirst` runs in CI, looks at the diff of a branch against its base, applies a
set of deterministic checks and returns an exit code. It writes nothing into
your repository, deploys nothing, and calls no model.

The property everything else rests on: **all code that judges comes from the
base branch, all code being judged comes from the head branch.** A branch cannot
soften the rules it is checked against, because its edits to the config and the
hooks are read from base and its own copies are ignored. The workflow is the one
file the branch can still reach, which is why it sits in `paths.protected` and
behind branch protection.

It does not know who wrote the diff. A patch from an agent in a sandbox and one
typed by hand look identical to it.

## What a verdict looks like

Tier 2, where the hooks run your tests:

```
redfirst v0.1.0 · verify · tier=2 · config=base:redfirst.toml · env_mode=reused · probe_runs=3
base f3c9a11  head 8b2e740  diff 4 files (+87 -12), tests 1 file (+31)

PASS  protected-paths
PASS  diff-budget            3/20 files, +56/800, -12/400
PASS  test-immutability      cases · 0 removed, 0 regressed
PASS  new-test-present       src/lib/order/total.test.ts
PASS  no-disabled-tests
WARN  suppression-scan       1 added line to review
PASS  suite-green            412 passed, 1 flaky (untouched)
FAIL  red-green              test is green on base, it does not cover the fix
      case "promo item does not break the total"  base runs: pass, pass, pass
      remediation: fix-test

N/A   blast-radius           no stack trace provided

WARN  guarded-paths          pnpm-lock.yaml
WARN  flaky                  tests/integration/webhook.spec.ts passed on retry, not touched by diff

REVIEW NEEDED
  src/lib/order/total.ts:42   } catch (e) {}

VERDICT: FAIL (1 gate)  exit=1
```

The new test passed on the base branch, where the fix does not exist. It caught
nothing. The failure line names the case rather than the file, because in a file
holding twenty cases "the file went red" tells a reviewer nothing.

Tier 1, with no hooks and no config, is the same command:

```
redfirst v0.1.0 · verify · tier=1 · config=defaults
base f3c9a11  head 8b2e740  diff 24 files (+910 -43), tests 1 file (+0)

PASS  protected-paths
WARN  diff-budget            23/20 files, +910/800, -12/400
      23 files, the budget allows 20
      910 added lines, the budget allows 800
FAIL  test-immutability      append-only · 1 removed
      src/lib/order/total.test.ts  test file deleted
      remediation: revert-paths
SKIP  new-test-present       tests.require_new is false
PASS  no-disabled-tests
PASS  suppression-scan

N/A   suite-green            no hooks, add .redfirst/ to enable
N/A   red-green              no hooks, add .redfirst/ to enable
N/A   blast-radius           no stack trace provided

VERDICT: FAIL (1 gate)  exit=1
```

An incomplete set of checks is a valid set. What got checked got a real check,
and the rest says `N/A` with the reason. Exit code 0 with an `N/A` in the report
is a normal result.

## Start with your own history

```
redfirst audit --last 20
```

It reads what your repository has already merged, applies the static gates to
those diffs and prints a summary. It installs nothing, adds no file, and
executes none of your code. Here is this repository's own history:

```
Audited 20 commits on origin/main (last 1 day)
  unit=commit · a PR merged as several commits counts several times

  5  diff-budget
  7  test-immutability
  1  no-disabled-tests
  4  suppression-scan
  2  guarded-paths
```

Add `--per-pr` for the breakdown, and `--per-pr --report csv` to get the
distribution your budget should be set from. The header names the unit it could reconstruct:
`merge` gives exact pull request boundaries, and the other two count a pull
request merged as three commits three times. Nobody chooses that; your
repository's merge settings do, and the header says so rather than keeping
quiet.

## Adoption in three tiers

Nothing gets rewritten between them. Each tier adds files the binary discovers
on its own, and the CI command is the same command.

| | Tier 0 | Tier 1 | Tier 2 |
|---|---|---|---|
| Files added | none | one workflow | workflow plus 3 to 5 scripts |
| Config | not needed | not needed | optional |
| Runs your tests | no | no | yes |
| Catches a fake test | no | no | **yes** |
| Time to adopt | a minute | 10 minutes | 20 minutes and up |

**Tier 1** is one workflow:

```
redfirst init --ci
```

That writes `.github/workflows/redfirst.yml` with the version and the SHA256 of
the binary pinned together, so a moved tag stops the install instead of judging
your diff with a binary nobody chose. From then on every static gate runs on
every pull request: `protected-paths`, `test-immutability` and
`no-disabled-tests` block, `diff-budget` and `suppression-scan` report,
`new-test-present` stays off until you ask for it, and `red-green` and
`suite-green` say `N/A` with the line that would switch them on.

The pull request that adds that file is the one diff redfirst has to refuse: the
path is protected from its first run, and GitHub runs the workflow from the
branch that added it. Push the file to your base branch, or merge that one pull
request with the check bypassed. Everything after it is judged normally.

**Tier 2** adds `.redfirst/`:

```
redfirst init --hooks go        # or node-unit, node-postgres-docker, python-pytest, blank, auto
```

The workflow does not change. The binary finds the hooks on the base branch and
turns on the two gates that need to run your tests. `.redfirst/**` is protected
too, so the pull request that adds it lands the same way the workflow did.

Once the hooks are on the base branch, run:

```
redfirst doctor
```

It executes them and tells you what would go wrong before a verdict rests on
them, which is worth one minute of your time: a hook that ignores its filter, a
runner that replays a cached result and results without per-case names all look
perfectly healthy by hand. It reads the hooks from base, the same place a
verdict reads them from, so run it after the merge rather than before. See
[docs/HOOKS.md](docs/HOOKS.md).

**A config is optional at every tier.** Without one the built-in defaults apply,
and they are written to stay out of a human's way. `redfirst init --config`
generates the stricter set meant for branches an agent writes. See
[docs/CONFIG.md](docs/CONFIG.md).

## The checks

They run in this order, and a static refusal cuts the run off before any of your
code executes.

| Gate | What it checks | Needs |
|---|---|---|
| `protected-paths` | No file in the diff touches `paths.protected`; `paths.allowed` holds when set | the diff |
| `diff-budget` | Files and lines stay inside `[budget]`, tests and generated files excluded | the diff |
| `test-immutability` | Existing tests are not deleted, hollowed out or bent. Three modes, see below | the diff, and hooks in `cases` mode |
| `new-test-present` | The diff adds a line to a file that holds test cases. Off by default | the diff |
| `no-disabled-tests` | No added line in a test file matches `tests.forbid_disabled`: `.skip(`, `.only(`, `xit(`, `xdescribe(` | the diff |
| `suppression-scan` | Added lines matching `[suppression]` reach the report. Warning only | the diff |
| `suite-green` | The whole suite passes on head, and a failure the diff did not write is not charged to it | hooks |
| `red-green` | Every new test case is red on base and green on head, in every run | hooks, test files in the diff |
| `blast-radius` | Changed files reachable from a stack trace. Designed, not implemented in v1 | a stack trace |

`red-green` is the one worth the setup. It takes the test files of your diff,
puts them onto the base tree with **the fix left out**, and runs them
`probe_runs` times. Every case the diff added has to fail there, every time, and
then pass on head, every time. A case that passes without the fix does not cover
it. A case that fails only sometimes is a regression test worth nothing, and the
same-answer-every-time rule is what closes the one channel in the system where
randomness could produce a pass.

The unit is the case, not the file, because a fake case appended to an existing
test file is otherwise invisible: the diff adds no new file, and the gate would
have nothing to probe. The case names come from your test runner's own results,
not from parsing your sources. `redfirst` knows nothing about your language and
intends to keep it that way.

One consequence to know before you switch tier 2 on: a diff that touches the
test surface and adds no new case is refused with `test files changed but no new
test case appeared`. A refreshed snapshot, an edited fixture or a renamed test
counts as touching it, so a change like that needs either a new case beside it or
a human to wave it through.

`test-immutability` has three modes because they go blind in different places.
`append-only`, the default, refuses a test file that loses a line, and catches
an assertion quietly weakened. `cases` refuses a case that disappeared or went
red, and lets a rename through, which the line-based modes cannot. `strict`
refuses any edit at all.

## Exit codes

The split is the point. An orchestrator that blames the agent for a broken
docker-compose burns its retry budget on somebody else's problem.

| Code | Meaning | What to do |
|---|---|---|
| `0` | Every available gate passed | Send it to a human |
| `1` | A gate refused, and a legal diff exists | Retry with the report in context |
| `2` | Broken harness, or a repository that was already broken | Retry without penalty, alert after N |
| `3` | The config exists and is invalid | Alert a human, a retry is pointless |
| `4` | An internal `redfirst` error | Alert, and file an issue here |
| `5` | A refusal no diff can answer | Alert a human, a retry is pointless |

Exit code 5 exists for one case in practice: your harness returns no per-case
names and the diff modifies an existing test file, so `red-green` cannot tell an
added case from an existing one. No diff the agent writes changes that, and
returning `1` would burn N retries before escalating a task that was valid from
the start.

Every failing gate also carries a `remediation` value, so an orchestrator can
route on it without reading prose: `revert-paths`, `reduce-scope`, `add-test`,
`fix-test`, `fix-code`, `human-required`.

## Reading a refusal

A failure line says what is wrong rather than what got checked, and the lines
below it name the file, the case and the runs. The common ones:

| The report says | What to do |
|---|---|
| `1 file under paths.protected` | Revert that file. If the task cannot be done without it, a human decides |
| `24 files, the budget allows 20` | Split the change into several pull requests |
| `append-only · 1 removed` | Put the deleted lines back, or move the repository to `cases` mode if the change is a refactor |
| `cases · 1 removed` and `the case is gone on head` | Restore the case, or say out loud why it should go |
| `2 added lines disabling a test` | Drop the `.skip(` and fix what it was hiding |
| `test is green on base, it does not cover the fix` | Rewrite the test so it fails without the fix |
| `flaky test, base probe is not deterministic` | The probe disagreed with itself. Make the test deterministic before it is worth anything |
| `test is red on head, the fix does not make it pass` | The test is honest and the code is not fixed yet |
| `test files changed but no new test case appeared` | Add the case the change deserves, or get a human to wave a snapshot refresh through |
| `2 tests red on head` | Fix them. A failure the diff did not write is labelled as such and does not count against it |
| `cannot verify added cases: test results lack per-case names` | Nothing a diff can do. The harness has to report named cases, and `redfirst doctor` prints the command for your runner |

## Install

No release is tagged yet, so today the answer is [from
source](#from-source). Once one is, the rest of this section applies.

Download the binary for your platform from the [releases
page](https://github.com/timhartmann7/redfirst/releases), check it against the
published checksums, and put it on `PATH`:

```sh
version=v0.1.0            # the tag you want
asset=redfirst_${version#v}_linux_amd64.tar.gz
base=https://github.com/timhartmann7/redfirst/releases/download/$version
curl -fsSLO "$base/$asset"
curl -fsSL "$base/checksums.txt" | grep " $asset\$" | sha256sum --check --strict -
tar -xzf "$asset" redfirst
```

Platforms: `linux/amd64`, `linux/arm64`, `darwin/arm64`.

In a GitHub workflow, `redfirst init --ci` writes the job for you. There is also
a composite action for the case where you would rather pin the action than the
binary:

```yaml
- uses: actions/checkout@v7
  with:
    fetch-depth: 0        # the diff runs from the merge base
- uses: timhartmann7/redfirst@v0.1.0
  with:
    version: v0.1.0
```

### From source

```sh
go install github.com/timhartmann7/redfirst/cmd/redfirst@latest
```

A binary built this way knows no published release to pin, so `init --ci`
refuses to generate a workflow rather than generating one without a checksum in
it. Write the job by hand, or wait for the release.

## Do not install this through your agent

Installing `redfirst` by asking an agent to download the binary and set up CI is
forbidden and unsupported. The product thesis says the agent cannot touch what
judges it. An agent-driven install refutes that thesis at install time, makes
installations non-deterministic, and reads as a supply chain attack.

What you can and should distribute through `CLAUDE.md` is the **rules**, not the
install: no edits to existing tests, a red test on main required, the list of
protected paths, the diff budget. That is a request the agent may ignore, which
is why the binary exists.

[docs/AGENT_RULES.md](docs/AGENT_RULES.md) carries a block to paste into your
`CLAUDE.md` or `AGENTS.md`, along with what to change when your config differs.

## What redfirst does not do

The boundaries matter more than the features. All of this is out of scope by
choice:

- it does not orchestrate or launch an agent;
- it does not open or merge pull requests;
- it does not deploy;
- it calls no models and holds no LLM-based heuristics;
- it does not judge whether a fix is **correct**, only whether the rules hold;
- it knows nothing about specific languages and frameworks. TypeScript, Python
  and Go are glob patterns and shell scripts to it.

The last one deserves saying out loud. `redfirst` catches "the agent deleted a
test", "the agent touched the migrations", "the agent wrote a test that stays
green without the fix". It does not catch "the agent wrapped everything in
try/catch, the test goes red before and green after, and the feature is dead". A
human reviewer owns that case, and no deterministic check will ever close it.

## What it defends against

A lazy agent, not an attacker. An agent will not hunt for a bypass through
symlinks or swapped git objects. It takes the cheapest path: disable the test,
widen the denylist, add `.skip`, wrap the failing call in try/catch. The checks
are tuned for that, and access rights guard the rest.

| Threat | What closes it |
|---|---|
| The branch rewrites the config or the hooks | Both are read from base |
| The branch disables the CI job | The workflow sits in protected paths, plus branch protection |
| The branch swaps the binary | Version and checksum pinned in the workflow, workflow protected |
| The agent merges itself | Its token has no merge rights |
| The branch rewrites the instructions for future agents | `CLAUDE.md`, `AGENTS.md`, `.mcp.json` and the skill directories are protected by default |
| Head code executes during verification | Verification runs in a throwaway runner with no production access |

The second to last row is the quiet one. An edit to `CLAUDE.md` changes the
behaviour of every future agent in the repository while looking like a harmless
documentation tweak.

The last row is not optional. Hooks from base run tests, and tests execute code
from head. Arbitrary code from the branch under judgement runs during
verification, and that is unavoidable, so give that runner no production
credentials and no production network.

## What it never does to your repository

- No network calls at runtime, `init` included. A test proves it by checking
  that the binary links no network package at all.
- No state between runs: no cache, no database, no files in `$HOME`. The core is
  a pure function of `(repo, base, head)`, and a test compares the JSON of two
  runs on one input, timings aside.
- No writes into the repository under judgement. Only stdout, stderr and the
  report file.
- Every temporary directory is removed, including on the panic path, and
  teardown runs on the deadline and on `SIGINT` too.

## Commands

```
redfirst verify   [flags]   # judge one diff
redfirst audit    [flags]   # apply the static gates to history already merged
redfirst init     [flags]   # generate the workflow, the config, the hook stubs
redfirst doctor   [flags]   # what works now, what does not, and why
redfirst explain  [flags]   # the rules a verify run would apply
redfirst version
```

`redfirst verify -h` prints the flags. The ones worth knowing: `--base` and
`--head` name the two refs, `--report json --out report.json` writes the
machine-readable report ([`schema/report.v1.json`](schema/report.v1.json)),
`--no-hooks` forces static gates only, and `--gates` restricts the set.

## We run it on ourselves

This repository is judged by its own binary on every pull request, blocking,
under [its own `redfirst.toml`](redfirst.toml) and
[its own hooks](.redfirst/). The binary comes from the base branch, not from the
branch under judgement, which is the thesis applied to ourselves.

```
$ redfirst doctor
redfirst v0.1.0 · doctor · tier=2

  config      base:redfirst.toml
  hooks       base:.redfirst/ → red-green and suite-green run
  workflow    base:.github/workflows/redfirst.yml
  detected    go + go test, no docker-compose
  env mode    fresh · no env-reset.sh, so the services cycle around every run
  case names  92 cases named by test.sh
  filter      honoured · the two runs differ: cmd/redfirst/audit_test.go reported 92, internal/audit/audit_test.go reported 25
  reruns      fresh · run 1 took 16.973s, run 2 took 14.679s
```

It paid for itself on the first run. The `go` preset we ship reported a build
failure as a test case named `TestMain`, which made `red-green` refuse the
honest fix that adds a function together with the test for it, and one of our
own tests turned out to race a busy machine. Both were found by the tool
refusing this repository's own pull requests.

## Development

```
make check      # gofumpt, vet, golangci-lint, deadcode, go test -race, go mod tidy
make bench      # the hot path
make dist       # the three release targets, reproducible
```

[`docs/redfirst-spec.md`](docs/redfirst-spec.md) is the source of truth for
behaviour, and [`CLAUDE.md`](CLAUDE.md) is how work in this repository is
supposed to be done. Both are worth reading before opening a pull request.

## License

MIT. See [LICENSE](LICENSE).
