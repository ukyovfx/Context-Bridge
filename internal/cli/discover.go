package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func runDiscover(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("discover", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "explicit discovery root")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *root == "" {
		return errors.New("discover requires --root <path>")
	}
	store, err := registryStore()
	if err != nil {
		return emitDiscoveryFailure(stdout, *root, *jsonOutput, workspace.ReasonRegistryReadFailed)
	}
	registryValue, err := store.Load()
	if err != nil {
		return emitDiscoveryFailure(stdout, *root, *jsonOutput, workspace.ReasonRegistryReadFailed)
	}
	result, err := (workspace.Discoverer{}).Discover(workspace.DiscoveryOptions{Root: *root, Registry: registryValue})
	if err != nil {
		return emitDiscoveryFailure(stdout, *root, *jsonOutput, workspace.ReasonAccessDenied)
	}
	emitDiscoveryResult(stdout, result, *jsonOutput)
	if result.Status == workspace.DiscoveryFailed {
		return errors.New("discovery failed")
	}
	return nil
}

func emitDiscoveryFailure(stdout io.Writer, requestedRoot string, jsonOutput bool, reason string) error {
	result := workspace.DiscoveryResult{
		RequestedRoot:      requestedRoot,
		Status:             workspace.DiscoveryFailed,
		Items:              []workspace.InventoryItem{},
		Warnings:           []workspace.DiscoveryIssue{},
		Errors:             []workspace.DiscoveryIssue{{Path: requestedRoot, Reason: reason}},
		AccessFailures:     []workspace.DiscoveryFailure{},
		ArtifactIndicators: []workspace.ArtifactIndicator{},
	}
	emitDiscoveryResult(stdout, result, jsonOutput)
	return fmt.Errorf("discovery failed: %s", reason)
}

func emitDiscoveryResult(stdout io.Writer, result workspace.DiscoveryResult, jsonOutput bool) {
	if jsonOutput {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stdout, "{\"requested_root\":%q,\"status\":%q,\"errors\":[{\"reason\":%q}]}\n", result.RequestedRoot, workspace.DiscoveryFailed, "JSON_ENCODE_FAILED")
			return
		}
		fmt.Fprintf(stdout, "%s\n", data)
		return
	}
	for _, item := range result.Items {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%v\tlocal-evidence-only: unique-vs-known-remotes=%d ref-oids-only-here=%d related-clones=%d\n", item.Probe.CanonicalPath, item.Probe.Topology, item.Role, item.States, item.Probe.LocalUniqueCount, item.RefOIDsOnlyHereCount, item.RelatedCloneCount)
	}
	for _, failure := range result.AccessFailures {
		fmt.Fprintf(stdout, "evidence-warning\t%s\t%s\n", failure.Path, failure.Reason)
	}
	for _, indicator := range result.ArtifactIndicators {
		fmt.Fprintf(stdout, "artifact-indicator\t%s\t%s\n", indicator.Path, indicator.Indicator)
	}
	if result.LimitReached {
		fmt.Fprintln(stdout, "evidence-warning\tdiscovery limit reached")
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(stdout, "discovery-warning\t%s\t%s\n", warning.Reason, warning.Path)
	}
	for _, issue := range result.Errors {
		fmt.Fprintf(stdout, "discovery-error\t%s\t%s\n", issue.Reason, issue.Path)
	}
	fmt.Fprintf(stdout, "discovery terminal_status=%s root=%s candidates=%d skipped=%d traversal_limit_reached=%t\n", result.Status, result.RootOrRequestedRoot(), result.CandidateCount, result.SkippedCount, result.TraversalLimitReached)
}
