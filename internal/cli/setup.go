package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

func runSetup(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "perform read-only checks only")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("setup accepts no positional arguments")
	}
	if _, err := lookPath("git"); err != nil {
		return err
	}
	if _, err := runOutput("", "git", "--version"); err != nil {
		return err
	}
	if _, err := runOutput("", "git", "config", "--get", "user.name"); err != nil {
		return errors.New("Git user.name is not configured")
	}
	if _, err := runOutput("", "git", "config", "--get", "user.email"); err != nil {
		return errors.New("Git user.email is not configured")
	}
	identity, err := readGitHubIdentity()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "setup ok: git configured; GitHub personal account %s authenticated; no credentials stored\n", identity.Login)
	if *dryRun {
		fmt.Fprintln(stdout, "dry-run: setup is read-only; zero mutations performed")
	}
	return nil
}
