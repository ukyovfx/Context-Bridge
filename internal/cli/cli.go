package cli

import (
	"errors"
	"fmt"
	"io"
)

const productLine = "Context Bridge — GPT ↔ Codex Development Context Bridge"

func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "setup":
		err = runSetup(args[1:], stdout, stderr)
	case "init":
		err = runInit(args[1:], stdout, stderr, version)
	case "doctor":
		err = runDoctor(args[1:], stdout, stderr)
	case "registry":
		err = runRegistry(args[1:], stdout, stderr)
	case "guard":
		err = runGuard(args[1:], stdout, stderr)
	case "discover":
		err = runDiscover(args[1:], stdout, stderr)
	case "handoff":
		err = runHandoff(args[1:], stdout, stderr)
	case "version":
		if len(args) != 1 {
			err = errors.New("version accepts no arguments")
		} else {
			fmt.Fprintf(stdout, "contextbridge %s\n", version)
		}
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		fmt.Fprintf(stderr, "contextbridge: %v\n", err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, productLine)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  contextbridge setup [--dry-run]")
	fmt.Fprintln(w, "  contextbridge init <project> [--root PATH] [--owner USER] [--profile core|openai] [--local-only] [--dry-run]")
	fmt.Fprintln(w, "  contextbridge doctor [--project PATH]")
	fmt.Fprintln(w, "  contextbridge registry list")
	fmt.Fprintln(w, "  contextbridge registry show <project>")
	fmt.Fprintln(w, "  contextbridge registry register <path> [--role canonical|alternate] [--dry-run]")
	fmt.Fprintln(w, "  contextbridge guard --project <id-or-alias> [--workspace <id-or-path>]")
	fmt.Fprintln(w, "  contextbridge discover --root <path> [--json]")
	fmt.Fprintln(w, "  contextbridge handoff <project> --task \"<intent>\" [--agent codex|claude|cursor] [--json]")
	fmt.Fprintln(w, "  contextbridge version")
}
