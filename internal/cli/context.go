package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/contextscan"
)

func runInspect(args []string, stdout, stderr io.Writer) error {
	path, jsonOutput, err := contextPathFlags("inspect", args, stderr)
	if err != nil {
		return err
	}
	report, err := contextscan.Inspect(path)
	if err != nil {
		return err
	}
	if jsonOutput {
		return emitJSON(stdout, report)
	}
	fmt.Fprintf(stdout, "inspect: %s\nmode: %s\n", report.Root, report.Mode)
	if report.CodexReady {
		fmt.Fprintln(stdout, "codex integration: READY (project AGENTS.md routes to Context Bridge)")
	} else {
		fmt.Fprintf(stdout, "codex integration: NOT_READY (%s)\n", report.Agents)
	}
	for _, system := range report.Systems {
		fmt.Fprintf(stdout, "found: %s (%s)\n", system.Name, system.Path)
	}
	for _, canonical := range report.Canonical {
		fmt.Fprintf(stdout, "canonical: %s\n", canonical)
	}
	for _, conflict := range report.Conflicts {
		fmt.Fprintf(stdout, "conflict: %s\n", conflict)
	}
	return nil
}
func runPreview(args []string, stdout, stderr io.Writer) error {
	path, jsonOutput, err := contextPathFlags("preview", args, stderr)
	if err != nil {
		return err
	}
	proposal, err := contextscan.Preview(path)
	if err != nil {
		return err
	}
	return emitContextProposal(stdout, proposal, jsonOutput)
}
func runContextApply(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("apply requires <path>")
	}
	path := args[0]
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	confirm := flags.Bool("confirm", false, "confirm the exact previewed scaffold")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected apply arguments")
	}
	proposal, err := contextscan.Preview(path)
	if err != nil {
		return err
	}
	if len(proposal.Changes) == 0 {
		return emitContextProposal(stdout, proposal, *jsonOutput)
	}
	if !*confirm {
		_ = emitContextProposal(stdout, proposal, *jsonOutput)
		return errors.New("confirmation required; review preview and rerun with --confirm")
	}
	if err := contextscan.Apply(proposal); err != nil {
		return err
	}
	if *jsonOutput {
		return emitJSON(stdout, map[string]any{"status": "applied", "proposal": proposal})
	}
	fmt.Fprintln(stdout, "applied: exact previewed Context Bridge scaffold")
	return nil
}
func runContextDoctor(args []string, stdout, stderr io.Writer) error {
	path, jsonOutput, err := contextPathFlags("doctor", args, stderr)
	if err != nil {
		return err
	}
	report, err := contextscan.Doctor(path)
	if err != nil {
		return err
	}
	if jsonOutput {
		return emitJSON(stdout, report)
	}
	fmt.Fprintf(stdout, "doctor: %s\nmode: %s\n", report.Inspection.Root, report.Inspection.Mode)
	if report.Inspection.CodexReady {
		fmt.Fprintln(stdout, "diagnostic: CODEX_INTEGRATION_READY")
	}
	if len(report.Findings) == 0 {
		fmt.Fprintln(stdout, "doctor ok: no context safety or routing findings")
		return nil
	}
	for _, finding := range report.Findings {
		fmt.Fprintf(stdout, "finding: %s %s\n", finding.Path, finding.Rule)
	}
	return nil
}
func contextPathFlags(command string, args []string, stderr io.Writer) (string, bool, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	path := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		path = args[0]
		args = args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return "", false, err
	}
	if flags.NArg() != 0 {
		return "", false, fmt.Errorf("unexpected %s arguments", command)
	}
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return "", false, err
		}
	}
	return path, *jsonOutput, nil
}
func emitContextProposal(stdout io.Writer, proposal contextscan.Proposal, jsonOutput bool) error {
	if jsonOutput {
		return emitJSON(stdout, proposal)
	}
	fmt.Fprintf(stdout, "preview: %s\nreason: %s\n", proposal.Root, proposal.Reason)
	if len(proposal.Changes) == 0 {
		fmt.Fprintln(stdout, "no changes needed")
		return nil
	}
	for _, change := range proposal.Changes {
		fmt.Fprintf(stdout, "\n--- %s (new)\n+++ %s\n", change.Path, change.Path)
		for _, line := range strings.Split(strings.TrimSuffix(change.Content, "\n"), "\n") {
			fmt.Fprintf(stdout, "+%s\n", line)
		}
	}
	return nil
}
func emitJSON(stdout io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(data))
	return err
}
