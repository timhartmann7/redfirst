package cliux

import "github.com/timhartmann7/redfirst/internal/harness"

// The node-postgres-docker preset, the one set of hooks with a service layer.
//
// It is the preset that has to answer the questions the two-layer environment
// asks and the others never do: where the service definition lives, how two
// branches checked at once stay off each other's ports, and what a reset means
// when the schema may not move. The answers are here rather than beside the
// presets that bring nothing up.
//
// One fact decides most of them. redfirst runs env-up.sh, env-reset.sh and
// env-down.sh from a copy of the hook directory, and only test.sh from the
// working copy: the tree under judgement is not there, and in reused mode it
// does not exist yet when the services come up.

// postgresCompose defines the services. It lives beside the hooks rather than
// at the repository root, and that is not tidiness: redfirst runs the service
// hooks from a copy of this directory, and the tree under judgement is not
// there. Base decides what the services are, the same way base decides the
// rules.
const postgresCompose = `services:
  postgres:
    # Match the version production runs. A schema that only applies on one of
    # the two proves nothing about the other.
    image: postgres:18-alpine
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: app
    # env.sh derives the host port from $REDFIRST_RUN_ID, so two branches
    # checked at once do not fight over it and both hooks agree on the number
    # without passing anything.
    ports:
      - "${PGPORT}:5432"
    # env-up.sh waits on this. Without it the first test run races the server's
    # startup and the failure looks like the agent's.
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres -d app"]
      interval: 1s
      timeout: 3s
      retries: 30
`

// postgresEnv is sourced ahead of every other hook, which is what lets the
// service layer and the working copy layer agree on how to reach the database.
//
// It computes rather than asks. The service hooks run in the hook directory and
// test.sh runs in the working copy, so no hook can read a file another one
// wrote, and a port derived from the run id is the same number on both sides.
const postgresEnv = `
# One compose project per run id: two branches checked at once would otherwise
# fight over the same containers, networks and volumes. Compose derives all
# three names from this one variable.
COMPOSE_PROJECT_NAME="redfirst-${REDFIRST_RUN_ID}"
export COMPOSE_PROJECT_NAME

# The host port, from the run id and nothing else. cksum is in POSIX, and the
# range stays out of the ephemeral ports the operating system hands out.
PGPORT=$(( 30000 + $(printf '%s' "$REDFIRST_RUN_ID" | cksum | cut -d' ' -f1) % 20000 ))
PGHOST=127.0.0.1
PGUSER=postgres
PGPASSWORD=postgres
PGDATABASE=app
export PGPORT PGHOST PGUSER PGPASSWORD PGDATABASE
export DATABASE_URL="postgres://${PGUSER}:${PGPASSWORD}@${PGHOST}:${PGPORT}/${PGDATABASE}"
`

const postgresUp = toolCheck + `command -v docker >/dev/null 2>&1 || {
	echo "docker is not on PATH" >&2
	exit 1
}

# No -f: this hook runs in the copy of ` + harness.HooksDir + `/, which is where
# compose.yaml sits. The tree under judgement is not here, and in reused mode it
# does not exist yet.
#
# --wait returns once the healthcheck in compose.yaml passes.
docker compose up --detach --wait
`

// postgresReset is what picks the reused mode: its presence tells redfirst the
// services may stay up between runs.
const postgresReset = `
# Data only. The base tree and the head tree share a schema for as long as the
# migrations sit in paths.protected, where the diff under judgement cannot reach
# them, and recreating the schema between runs would then pay for a migration
# nothing changed. The built-in defaults only guard the migrations, which reports
# without blocking, so move the path into paths.protected before relying on this
# file; redfirst doctor prints a reuse row while it sits outside.
#
# The bookkeeping table of the migration tool stays: emptying it would tell the
# next run the schema was never applied.
#
# Run from the hook directory, beside compose.yaml, like every service hook.
#
# A quoted heredoc, and that is load-bearing rather than taste: inside shell
# single quotes a doubled '' closes the quote and opens it again instead of
# reaching SQL, so the quotes around every literal below would be eaten before
# psql saw them. <<'SQL' expands nothing at all, which is what leaves $$, the
# quotes and the % patterns to arrive as written.
#
# ON_ERROR_STOP=1 makes a failed statement a failed script. Without it psql
# prints the error and exits 0, redfirst reads a reset that worked, and the next
# run finds the data of the one before.
#
# RESTART IDENTITY resets the sequences the truncated tables own, so the first
# row of the head phase gets the id the first row of the base phase got. A test
# that asserts an id is a bad test, and a probe that goes red on base for that
# reason is a false refusal.
docker compose exec -T postgres psql -U "$PGUSER" -d "$PGDATABASE" -q \
	-v ON_ERROR_STOP=1 <<'SQL'
DO $$
DECLARE t text;
BEGIN
  FOR t IN
    SELECT tablename FROM pg_tables
    WHERE schemaname = 'public' AND tablename NOT LIKE '%migration%'
  LOOP
    EXECUTE format('TRUNCATE TABLE %I RESTART IDENTITY CASCADE', t);
  END LOOP;
END $$;
SQL
`

const postgresTest = `
# Once per phase: the working copy is shared by every run inside it, and the
# schema lives in the services, which outlive the copy.
if [ "$REDFIRST_PROBE_INDEX" = 1 ]; then
	pnpm install --frozen-lockfile
	pnpm migrate
fi

# $REDFIRST_FILTER is left unquoted on purpose: it carries a list of files, and
# an empty one has to run the whole suite. --no-cache keeps every probe honest.
pnpm exec vitest run --no-cache \
	--reporter=junit --outputFile="$REDFIRST_RESULTS" $REDFIRST_FILTER
`

const postgresDown = `
# --volumes: the data of this run leaves with it. The volume name carries the
# run id through COMPOSE_PROJECT_NAME, so this stops nobody else's database.
docker compose down --volumes --remove-orphans
`
