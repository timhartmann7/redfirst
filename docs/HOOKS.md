# `.redfirst/` hooks

Tier 1 judges a diff without running anything. Tier 2 runs your tests, and that
is what buys the two checks the tool exists for: `red-green`, which proves a new
test fails without the fix, and `suite-green`, which proves the branch is not
red.

Everything on this page is optional. Without `.redfirst/` those two gates report
`N/A` and the verdict on the rest stays valid.

`redfirst init --hooks <preset>` writes a working set for the common stacks, and
`--hooks auto` picks one from your repository's own root markers. Read this page
when you are adapting one, when `redfirst doctor` says something you did not
expect, or when your stack is not in the list.

## The five scripts

They live in `.redfirst/` and are read **from the base ref**, like the config.
redfirst copies the directory into a temporary run directory and executes the
copy, so an edit on the branch under judgement changes nothing about how that
branch is judged. Keep `.redfirst/**` in `paths.protected`, which the defaults
already do.

| Script | Required | Runs in | Job |
|---|---|---|---|
| `env-up.sh` | yes | the copy of `.redfirst/` | Bring the services up. Exit 0 once they answer |
| `test.sh` | yes | the working copy | Run the tests. Exit 0 when they pass |
| `env-down.sh` | yes | the copy of `.redfirst/` | Stop what `env-up.sh` started |
| `env-reset.sh` | no | the copy of `.redfirst/` | Reset the data between runs. Its presence picks the reused mode |
| `env.sh` | no | sourced into each hook | Export variables the other hooks share |

A project with no external services leaves `env-down.sh` as an `exit 0` stub and
keeps `env-up.sh` down to a check that the toolchain is there. That is what the
`node-unit`, `python-pytest` and `go` presets do.

Three rules the copy imposes and people trip over:

- **The scripts need the executable bit in git.** `git update-index
  --chmod=+x .redfirst/test.sh` if you created them by hand. A hook without it
  is exit code 2 with the specific message, not a mysterious failure.
- **Half a hook set is a broken harness.** A base ref with `.redfirst/` but no
  `test.sh` says the project meant tier 2 and got it wrong, so redfirst refuses
  with exit code 2 rather than quietly dropping the two gates.
- **The service hooks do not see your repository.** They run in the copy of
  `.redfirst/`, and in reused mode they run before any working copy exists. A
  file they read has to sit beside them in `.redfirst/`. That is why the
  `node-postgres-docker` preset keeps its `compose.yaml` there rather than at
  the repository root.

## What each hook gets

Hooks inherit the environment of the redfirst process, plus:

| Variable | Given to | Value |
|---|---|---|
| `REDFIRST_RUN_ID` | every hook | One id per redfirst run, shared by every phase and probe. Of the form `redfirst-<random>` |
| `REDFIRST_WORKDIR` | `test.sh` | The working copy. It is already the current directory |
| `REDFIRST_FILTER` | `test.sh` | Space separated file paths, or empty. Empty means run everything. The list is the test surface of the diff, so snapshots, `testdata/` files and runner config files can appear in it |
| `REDFIRST_RESULTS` | `test.sh` | Where to write the results file. It has no extension |
| `REDFIRST_PHASE` | `test.sh` | `base`, `head` or `suite` |
| `REDFIRST_PROBE_INDEX` | `test.sh` | Which run this is inside the working copy, counting from 1 |

`env.sh` is sourced in the shell that then executes the hook, so what it exports
reaches that hook. It carries no `set -e` and no `exit`: it is sourced, and
ending the shell that sourced it would end the hook before it ran. redfirst
checks that at startup and refuses with `env.sh ends the shell that sources it,
so no hook would run after it` rather than reading the silence as a pass.

Standard output and standard error of a hook go to a file, and the last 4 KB of
it follows the error message when a hook whose contract is exit 0 exits
otherwise, or when a hook cannot be started at all. So print what went wrong in
`env-up.sh`, `env-reset.sh` and `env-down.sh`: a hook that fails silently leaves
the report with nothing to say.

A non-zero `test.sh` is a verdict rather than an error, so its output stays in
the run directory and the report names the failing cases out of
`$REDFIRST_RESULTS` instead.

## The four rules for `test.sh`

Each of them is one silent way to make `red-green` worthless. A hook can break
any of them and still look perfectly healthy by hand.

**1. Honour `$REDFIRST_FILTER`.** It carries a list of files. Run only those
when it is non-empty, and the whole suite when it is empty. Leave it unquoted so
an empty value disappears:

```sh
pnpm exec vitest run --reporter=junit --outputFile="$REDFIRST_RESULTS" $REDFIRST_FILTER
```

A hook that ignores it still works, and every probe then runs your whole suite
three times over. `redfirst doctor` measures this and says so.

**2. Write per-case named results to `$REDFIRST_RESULTS`.** JUnit XML or TAP.
The content picks the format, not the extension: a file that starts with `<` is
read as XML, anything else as TAP.

This is the one requirement redfirst cannot work around. Without names it cannot
tell a case the diff added from one that was already there, so a diff that
modifies an existing test file gets exit code 5, which no diff the agent writes
can answer. Parsing your sources instead would mean a parser per language, and
that is the knowledge redfirst refuses to hold.

**3. Install dependencies only when `$REDFIRST_PROBE_INDEX` is 1.** Every run
inside a phase shares one working copy, so an install repeated per run buys
nothing:

```sh
if [ "$REDFIRST_PROBE_INDEX" = 1 ]; then
	pnpm install --frozen-lockfile
	pnpm build
fi
```

Without the guard, `probe_runs = 3` means one install per run: three in the base
phase, three in the head phase, one more in the base phase when the diff touches
a test file base already has, and one for every suite run on top.

One kind of step has to stay outside the guard: anything that compiles the test
sources ahead of the run. The base phase writes the head versions of those files
into its working copy between the inventory run and the first probe, so a
compile that happened on run 1 alone would have used the versions from before
the overlay, and the probes would then judge code nobody asked about. Building
the application, which is what the snippet above does, is safe inside the guard.
Precompiling the tests is not.

**4. Never hand back the result of the previous run.** Runners that cache have a
flag for it. `go test` needs `-count=1`, `vitest` wants `--no-cache`. Three
probes that collapse into one leave `probe_runs` meaning nothing, and the
identical result the gate demands is what stands between a fake test and a green
verdict.

One more thing that is not a rule but ends the same way: never add
`--passWithNoTests` or its equivalent. It turns a filter that matched nothing
into a green run, which is the exact silent pass `red-green` exists to catch.

## Results: JUnit and TAP

**JUnit.** Any `<testcase>` element anywhere in the file counts, whether the
root is `<testsuites>` or a bare `<testsuite>`.

| Attribute or child | Read as |
|---|---|
| `classname` | The file the case belongs to |
| `name` | The case name |
| child `<failure>` or `<error>` | The case failed |
| child `<skipped>` | The case was skipped |
| neither | The case passed |

Everything else, including the `tests=` and `failures=` counters, is ignored: a
file with counters and no `<testcase>` reports zero cases.

If your runner writes the source path into `classname`, `suite-green` can tell a
failure the diff wrote from one it inherited. If it writes a package or module
name instead, which `gotestsum` and pytest do, that distinction is lost and an
inherited failure costs one extra suite run on base before the verdict comes
out. The verdict itself stays correct.

**TAP.** Only lines that start at column 0 count:

```
ok 1 - adds prices
not ok 2 - applies a discount
ok 4 # skip nothing to round
```

The number is dropped, a leading `-` is trimmed, and the rest is the case name.
A `# skip` directive makes the case a skip whatever the keyword said. Indented
lines are subtests, and they are ignored because the test that owns them reports
again at the margin. TAP names no file, so `suite-green` treats every TAP
failure as one the diff may have written.

**A missing or empty results file is not an error.** It costs the run its
per-case names, and the gates answer accordingly. A file that opens as XML and
then breaks is a different thing: it claims to be results, and half of one means
the runner died mid-write. That is exit code 2.

## Phases, runs and the working copy

A **phase** is one half of `red-green`, or the `suite` run that `suite-green`
needs. Its working copy is created from scratch.

A **run** is one execution of `test.sh` inside a phase. Every run of a phase
shares one working copy.

| Phase | Tree | Runs | Filter |
|---|---|---|---|
| `base` | The merge base | 1 inventory run, then `probe_runs` runs with the head test files overlaid | The test surface of the diff |
| `head` | The head commit | `probe_runs` | The test surface of the diff |
| `suite` | The head commit | 1, plus `suite.retry_failed` reruns of what failed | Empty, then the files of the failures |
| `suite` again | The merge base | 1, and only when an untouched failure survived the retry and nothing the diff touched failed | Empty |

`REDFIRST_PROBE_INDEX` counts inside the working copy, from 1. The base phase
therefore numbers its inventory run 1 and its probes 2, 3, 4, which is what you
want: the install still happens once.

The two base filters differ in one way worth knowing when you read a run log.
The inventory names the files as base has them, so a file the diff adds is left
out of it, and the probes name them as head has them, so a file the diff deletes
is left out of those. Filtering a runner to a path that is not there asks it to
collect a file nobody wrote.

The inventory run only happens when the diff modifies test files that already
exist on base. A fix that adds one new test file skips it.

The order is fixed: base first, then head. State can leak forward only. Data the
base phase leaves behind can turn the head phase red, and the head phase expects
green, so a leak produces a refusal rather than a false pass. The reverse would
be a fake test walking through, and that is the direction the whole design
protects.

## Two layers, and why only one of them is reused

**Services** are the database, the cache, the broker, the API mocks. They are
expensive to start and safe to keep between runs as long as the data gets reset.

**The working copy** is the tree, the dependencies and the build output. It is
recreated for every phase, always, with no option to keep it. A `dist/`
directory compiled on base that survived into the head phase would turn a
nonexistent fix green.

That is why dependency installation and the build belong in `test.sh` and not in
`env-up.sh`.

## Environment modes

The presence of `.redfirst/env-reset.sh` picks the mode. There is no config flag
for it, and reuse without a reset does not exist.

| Mode | When | What runs |
|---|---|---|
| `fresh` | No `env-reset.sh`, or `--fresh-env` was passed | `env-up.sh` before every run, `env-down.sh` after it |
| `reused` | `env-reset.sh` is present | `env-up.sh` once, `env-reset.sh` between runs, `env-down.sh` at the end |

The mode reaches the first line of the report, so somebody digging into a
strange refusal sees it right away.

Reusing services is safe only while base and head share a database schema, and
that holds only while the migrations sit in `paths.protected`, where the diff
under judgement cannot reach them. The built-in defaults only guard migrations,
which reports without blocking, so add the path to `paths.protected` before you
rely on reuse. `redfirst doctor` prints a `reuse` row while they sit outside
it.

[`examples/postgres/env-reset.sh`](examples/postgres/env-reset.sh) is a
reference reset for Postgres: truncate the data, leave the schema and the
migration bookkeeping table alone.

## Teardown

`env-down.sh` runs on the panic path, on the deadline and on `SIGINT`. It gets
two minutes of its own after the run's deadline has passed, because a hung
teardown that kept redfirst alive would leave the containers it was supposed to
stop.

Write it accordingly. No `set -e`: half the environment may never have been
built, and stopping at the first command that finds nothing to stop would leak
the rest.

A hook that starts a process tree gets killed as a tree. redfirst puts every
hook in a process group of its own and signals the group, because killing the
script alone leaves the containers and the test runner behind.

## Isolation

`$REDFIRST_RUN_ID` is unique per redfirst run. Put it into every container,
network, volume and port name, or two branches judged at the same time on one
machine will fight over them.

The `node-postgres-docker` preset shows both halves of that:

```sh
COMPOSE_PROJECT_NAME="redfirst-${REDFIRST_RUN_ID}"
PGPORT=$(( 30000 + $(printf '%s' "$REDFIRST_RUN_ID" | cksum | cut -d' ' -f1) % 20000 ))
```

The port is computed rather than read from a file, because no hook can read what
another wrote: the service hooks run in the hook directory and `test.sh` runs in
the working copy. `env.sh` is what lets the two agree, since both source it.

## Exit codes from a hook

The split matters, and only the hooks can draw it.

- A non-zero exit from `env-up.sh`, `env-reset.sh` or `env-down.sh` is **exit
  code 2**: a broken harness, nobody's fault, retry without penalty.
- A non-zero exit from `test.sh` means the suite went red. That is a verdict,
  and it usually ends as **exit code 1**.

So put the toolchain check in `env-up.sh`. A missing `pnpm` or a database that
will not start is not a failing test, and `env-up.sh` is where the difference
can still be told:

```sh
command -v pnpm >/dev/null 2>&1 || {
	echo "pnpm is not on PATH" >&2
	exit 1
}
```

## Presets

`redfirst init --hooks <preset>` writes a set into `.redfirst/`. `--hooks auto`
lets the repository's own root markers pick one, and the command prints which.

| Preset | Writes | For |
|---|---|---|
| `node-unit` | `env-up.sh`, `test.sh`, `env-down.sh` | vitest or jest, no services |
| `node-postgres-docker` | those plus `compose.yaml`, `env.sh`, `env-reset.sh` | vitest with Postgres in docker compose |
| `python-pytest` | `env-up.sh`, `test.sh`, `env-down.sh` | pytest in a venv the hook builds |
| `go` | `env-up.sh`, `test.sh`, `env-down.sh` | `go test` through `gotestsum` |
| `blank` | three commented stubs | anything else |
| `auto` | whatever the root markers point at | letting the repository choose |

`init` never writes over an existing file. Move yours aside first.

The `blank` preset's `test.sh` exits 1 with a message until you fill it in.
That is deliberate: a repository not ready for tier 2 is better off with no
`.redfirst/` at all, which leaves the two gates unavailable and refuses nobody.

### Notes per runner

**vitest**: `--reporter=junit --outputFile="$REDFIRST_RESULTS"`, and
`--no-cache` so the transform cache in the shared working copy is not what
differs between the first probe and the third.

**jest**: `--reporters=jest-junit` with `JEST_JUNIT_OUTPUT_FILE` exported to
`$REDFIRST_RESULTS`.

**pytest**: `--junitxml="$REDFIRST_RESULTS"`, plus `--maxfail=0` to override an
`addopts` of `-x`. A report truncated at the first failure leaves `red-green`
unable to tell a case that never ran from one that disappeared. Export
`PYTHONDONTWRITEBYTECODE=1` as well: cached bytecode is keyed on a file's size
and mtime in whole seconds, two versions of one test file can collide on both,
and the probe would then execute the code of the run before it.

**go test**: `gotestsum --junitfile "$REDFIRST_RESULTS" --format testname --
-count=1 <packages>`. `go test` takes packages while the filter names files, so
map each path to the package that owns it, and remember that `testdata` and
fixture directories are not packages. `-count=1` is not optional: without it the
test cache replays the previous verdict and the probes collapse into one.

## Check your hooks before a verdict depends on them

```
redfirst doctor
```

It runs your hooks against the base ref and judges nothing. It exists because
three ways of breaking `red-green` cost nothing when you write them and
everything when a verdict rests on them, and none of them is visible from the
diff:

```
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

What each harness row can tell you:

- **case names**: `none`, `unnamed`, or how many it found. A `none` row says
  whether the hook also exited non-zero, because a suite that never built and a
  runner that writes no results are different fixes.
- **filter**: `honoured`, `ignored`, or `cannot tell`. It runs `test.sh` over two
  files in different directories and compares what came back. A repository whose
  test files all sit in one directory gets `cannot tell` rather than a guess.
- **reruns**: `fresh`, `unexplained gap`, or `cannot tell`. Two runs of a phase
  do the same work, so a second run ten times quicker than the first means
  either a toolchain went warm or the runner replayed its answer. A `test.sh`
  that branches on `$REDFIRST_PROBE_INDEX` has an innocent explanation, and
  doctor declines to read anything into the gap.
- **reuse**: only in reused mode, and only when migrations sit outside
  `paths.protected`.

Every finding names the specific command for your runner. `doctor` never changes
its exit code over a finding: it is read by a person deciding what to fix, not
by an orchestrator deciding whom to blame.
