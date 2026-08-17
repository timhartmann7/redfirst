// Command redfirst judges one diff and returns an exit code.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/timhartmann7/redfirst/internal/domain"
)

func main() {
	os.Exit(dispatch())
}

// dispatch keeps os.Exit out of run, so every defer down the call stack still
// fires. Teardown always runs, including on SIGINT.
func dispatch() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	if err != nil && !verdictError(err) {
		fmt.Fprintf(os.Stderr, "redfirst: %v\n", err)
	}
	return domain.ExitCode(err)
}

// verdictError reports whether the report already explained the failure, in
// which case stderr repeating it adds noise.
func verdictError(err error) bool {
	return errors.Is(err, domain.ErrGateViolation) || errors.Is(err, domain.ErrHumanRequired)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		if err := usage(stderr); err != nil {
			return err
		}
		return errors.New("no command given")
	}

	switch args[0] {
	case "verify":
		return verify(ctx, args[1:], stdout, stderr)
	case "version":
		_, err := fmt.Fprintln(stdout, domain.Version)
		return err
	}

	if err := usage(stderr); err != nil {
		return err
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func usage(w io.Writer) error {
	_, err := fmt.Fprint(w, `usage: redfirst <command> [flags]

  verify    judge one diff
  version   print the redfirst version

Run `+"`redfirst verify -h`"+` for the flags.
`)
	return err
}
