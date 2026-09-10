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
		return err
	}
	registryValue, err := store.Load()
	if err != nil {
		return err
	}
	result, err := (workspace.Discoverer{}).Discover(workspace.DiscoveryOptions{Root: *root, Registry: registryValue})
	if err != nil {
		return err
	}
	if *jsonOutput {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "%s\n", data)
		return err
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
	return nil
}
