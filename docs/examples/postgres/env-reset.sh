#!/bin/sh
# Reference env-reset.sh for a project whose tests need Postgres.
#
# Copy it into .redfirst/, adjust the two things marked below, and the services
# stop cycling: redfirst brings Postgres up once, resets the data between runs
# and takes it down at the end. On a suite that runs a dozen filtered probes,
# that is the difference between one container start and a dozen.
#
# The presence of this file is what picks the mode. There is no config flag for
# it, and there is no way to reuse services without resetting them: data the
# base phase left behind would decide the head phase.
#
# `redfirst init --hooks node-postgres-docker` writes an env-reset.sh of its own,
# in the docker form, together with the compose file, env.sh and the rest of the
# set. This copy exists for the projects that reach Postgres some other way: a
# service container in the CI workflow, a cluster on the host, a database
# somebody else runs.
set -eu

# Where the database is. env.sh runs before every hook and is the place to
# export this, so the service hooks and test.sh agree on one value without
# passing anything between them. Derive the port from $REDFIRST_RUN_ID if two
# branches can be judged at once on the same machine.
#
#   Adjust: the variable, if your env.sh calls it something else.
: "${DATABASE_URL:?env.sh has to export DATABASE_URL before the hooks run}"

# Data only. The schema stays where the migrations put it.
#
# This is not an optimisation. The base tree and the head tree share a schema
# only while the migrations sit in paths.protected, where the diff under
# judgement cannot reach them, and rebuilding the schema between runs would then
# pay a migration cost for nothing. The built-in defaults only guard migrations,
# which reports without blocking, so move the path into paths.protected before
# relying on this file. `redfirst doctor` prints a reuse row while they sit
# outside it, and the other answer is to delete this file and let the services
# cycle instead.
#
# The bookkeeping table of the migration tool is excluded for the same reason.
# Emptying it would tell the next run the schema was never applied, and that run
# would apply it a second time on top of itself.
#
#   Adjust: the exclusion pattern, to whatever your migration tool calls its
#   table. Prisma writes _prisma_migrations, golang-migrate schema_migrations,
#   Alembic alembic_version, Rails schema_migrations and ar_internal_metadata.
#
# A quoted heredoc, and that is load-bearing rather than taste: <<'SQL' expands
# nothing, which is what leaves $$, the quotes and the % patterns to reach psql
# as written.
#
# RESTART IDENTITY resets the sequences the truncated tables own, so the first
# row of the head phase gets the same id as the first row of the base phase. A
# test that asserts an id is a bad test, but a probe that goes red on base for
# that reason is a false refusal, and the flag costs nothing.
#
# ON_ERROR_STOP=1 makes a failed statement a failed script. Without it psql
# reports the error and exits 0, redfirst reads a reset that worked, and the
# next run finds the data of the one before.
psql "$DATABASE_URL" --quiet --no-psqlrc -v ON_ERROR_STOP=1 <<'SQL'
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

# Seed data, if the suite expects any, goes here rather than in test.sh: this
# script runs once between runs, and test.sh runs inside a working copy that
# knows nothing about what the run before it left in the database.
#
#   psql "$DATABASE_URL" --quiet -v ON_ERROR_STOP=1 --file seed.sql
#
# A Postgres reached through docker compose replaces the psql calls above with
# the same command inside the container, and drops the DATABASE_URL check:
#
#   docker compose exec -T postgres psql -U "$PGUSER" -d "$PGDATABASE" -q \
#     -v ON_ERROR_STOP=1 <<'SQL'
#
# Both forms run in the copy of .redfirst/, not in the working copy, so a file
# this script reads has to sit beside it in .redfirst/.
