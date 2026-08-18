package cliux_test

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/harness"
)

// The tests below belong to the one preset with a service layer. What the
// others cannot get wrong, it can: where the service definition is read from,
// how two branches checked at once stay off each other's ports, and what the
// shell does to a statement on its way to the database.

// TestInitHooks_DockerNamesCarryTheRunID covers the isolation requirement: two
// branches checked at once must not fight over a container, a network or a
// volume.
func TestInitHooks_DockerNamesCarryTheRunID(t *testing.T) {
	t.Parallel()

	files := written(t, t.TempDir(), cliux.PresetNodePostgres)

	if !strings.Contains(files["env.sh"], "REDFIRST_RUN_ID") {
		t.Errorf("no docker name carries the run id:\n%s", files["env.sh"])
	}
	if !strings.Contains(files["env-down.sh"], "--volumes") {
		t.Errorf("the teardown leaves the data of the run behind:\n%s", files["env-down.sh"])
	}
	if _, ok := files["env-reset.sh"]; !ok {
		t.Error("the docker preset carries no env-reset.sh, so its services cycle around every run")
	}
}

// TestInitHooks_TheDockerPresetShipsItsOwnComposeFile is what separates a
// docker preset that works from one that cannot run a single command.
//
// redfirst runs the service hooks from a copy of the hook directory, not from
// the repository: harness.Session hands env-up.sh, env-reset.sh and env-down.sh
// a working directory of .redfirst/, and in reused mode the working copy does
// not exist yet when the first of them runs. A compose file at the repository
// root is therefore not there to be found, and the two layers cannot pass a
// port to each other through a file either.
func TestInitHooks_TheDockerPresetShipsItsOwnComposeFile(t *testing.T) {
	t.Parallel()

	files := written(t, t.TempDir(), cliux.PresetNodePostgres)

	compose, ok := files["compose.yaml"]
	if !ok {
		t.Fatalf("the docker preset writes no compose file: %v", slices.Sorted(maps.Keys(files)))
	}
	if !strings.Contains(compose, "${PGPORT}") {
		t.Errorf("the compose file pins a host port, so two runs on one machine collide:\n%s", compose)
	}
	// Both layers derive the port from the run id, which is the one value they
	// share: neither can read what the other wrote.
	if !strings.Contains(files["env.sh"], "REDFIRST_RUN_ID") {
		t.Errorf("env.sh does not derive the port from the run id:\n%s", files["env.sh"])
	}
	for _, hook := range []string{"env-up.sh", "env-reset.sh", "env-down.sh"} {
		if !strings.Contains(files[hook], "docker compose") {
			t.Errorf("%s never reaches the services:\n%s", hook, files[hook])
		}
	}
}

// TestInitHooks_TheResetSQLReachesPsqlAsItWasWritten runs the generated hook
// against a docker that records what it was handed.
//
// Reading the template proves nothing here: the shell gets the text before psql
// does, and inside single quotes a doubled ” closes the quote and reopens it
// rather than reaching SQL. A reset whose literals lost their quotes is a
// syntax error, and env-reset.sh exiting non-zero is exit code 2 on every run
// of a reused environment.
func TestInitHooks_TheResetSQLReachesPsqlAsItWasWritten(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	written(t, root, cliux.PresetNodePostgres)

	// A docker that swallows its arguments and writes down its standard input,
	// which is where psql reads the statement from.
	bin := t.TempDir()
	sql := filepath.Join(root, "sql.txt")
	stub := "#!/bin/sh\ncat > " + sql + "\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write the docker stub: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), filepath.Join(root, harness.HooksDir, "env-reset.sh"))
	// The stub comes first on the path; the rest of it is what the hook needs
	// to be a shell script at all.
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "PGUSER=postgres", "PGDATABASE=app")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("env-reset.sh: %v\n%s", err, out)
	}

	got := string(mustRead(t, sql))
	for _, want := range []string{"schemaname = 'public'", "'%migration%'", "format('TRUNCATE TABLE %I CASCADE', t)"} {
		if !strings.Contains(got, want) {
			t.Errorf("the shell ate the quotes around %q before psql saw them:\n%s", want, got)
		}
	}
}
