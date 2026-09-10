package state

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)

type GitRunner interface {
	Output(directory string, args ...string) ([]byte, error)
}

type Provenance struct {
	Schema      int
	BasisBranch string
	BasisCommit string
	BasisDate   time.Time
}

type Report struct {
	ContentIntegrity string   `json:"content_integrity"`
	BasisValidity    string   `json:"basis_validity"`
	BasisFreshness   string   `json:"basis_freshness"`
	Reasons          []string `json:"reasons,omitempty"`
}

func Parse(path string) (Provenance, error) {
	file, err := os.Open(path)
	if err != nil {
		return Provenance{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() || scanner.Text() != "---" {
		return Provenance{}, errors.New("CURRENT-STATE front matter is missing")
	}
	values := map[string]string{}
	closed := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			closed = true
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return Provenance{}, errors.New("CURRENT-STATE front matter is invalid")
		}
		values[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return Provenance{}, err
	}
	if !closed {
		return Provenance{}, errors.New("CURRENT-STATE front matter is not closed")
	}
	schema, err := strconv.Atoi(values["contextbridge_state_schema"])
	if err != nil || schema != 1 {
		return Provenance{}, errors.New("CURRENT-STATE schema is unsupported")
	}
	provenance := Provenance{Schema: schema, BasisBranch: values["basis_branch"], BasisCommit: values["basis_commit"]}
	if values["basis_date"] != "" {
		provenance.BasisDate, err = time.Parse(time.RFC3339, values["basis_date"])
		if err != nil {
			return Provenance{}, errors.New("CURRENT-STATE basis_date is invalid")
		}
	}
	return provenance, nil
}

func Evaluate(project string, runner GitRunner) Report {
	report := Report{ContentIntegrity: "unverified", BasisValidity: "unverified", BasisFreshness: "unverified"}
	path := filepath.Join(project, "docs", "agent", "CURRENT-STATE.md")
	provenance, err := Parse(path)
	if err != nil {
		report.Reasons = append(report.Reasons, "provenance_missing_or_invalid")
		return report
	}
	status, statusErr := runner.Output(project, "status", "--porcelain=v2", "-z", "--", "docs/agent/CURRENT-STATE.md")
	if statusErr != nil {
		report.Reasons = append(report.Reasons, "content_integrity_unavailable")
	} else if len(status) == 0 {
		report.ContentIntegrity = "ok"
	} else {
		report.ContentIntegrity = "modified"
		report.Reasons = append(report.Reasons, "current_state_has_uncommitted_changes")
	}
	if provenance.BasisBranch == "" || !objectIDPattern.MatchString(provenance.BasisCommit) || provenance.BasisDate.IsZero() {
		report.Reasons = append(report.Reasons, "basis_fields_unverified")
		return report
	}
	if _, err := runner.Output(project, "cat-file", "-e", provenance.BasisCommit+"^{commit}"); err != nil {
		report.Reasons = append(report.Reasons, "basis_commit_missing")
		return report
	}
	if _, err := runner.Output(project, "merge-base", "--is-ancestor", provenance.BasisCommit, "refs/heads/"+provenance.BasisBranch); err != nil {
		report.Reasons = append(report.Reasons, "basis_commit_not_on_basis_branch")
		return report
	}
	report.BasisValidity = "valid"
	branch, err := runner.Output(project, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(string(branch)) != provenance.BasisBranch {
		report.BasisFreshness = "stale"
		report.Reasons = append(report.Reasons, "current_branch_differs_from_basis")
		return report
	}
	changed, err := runner.Output(project, "diff", "--name-only", provenance.BasisCommit+"..HEAD")
	if err != nil {
		report.Reasons = append(report.Reasons, "basis_diff_unavailable")
		return report
	}
	for _, changedPath := range strings.Fields(strings.ReplaceAll(string(changed), "\\", "/")) {
		if changedPath != "docs/agent/CURRENT-STATE.md" && changedPath != ".contextbridge/manifest.json" {
			report.BasisFreshness = "stale"
			report.Reasons = append(report.Reasons, fmt.Sprintf("project_changed_after_basis:%s", changedPath))
			return report
		}
	}
	workingTree, err := runner.Output(project, "status", "--porcelain=v2", "-z", "--untracked-files=normal")
	if err != nil {
		report.Reasons = append(report.Reasons, "working_tree_freshness_unavailable")
		return report
	}
	for _, record := range strings.Split(string(workingTree), "\x00") {
		changedPath := porcelainPath(record)
		if changedPath != "" && changedPath != "docs/agent/CURRENT-STATE.md" {
			report.BasisFreshness = "stale"
			report.Reasons = append(report.Reasons, fmt.Sprintf("uncommitted_project_change:%s", changedPath))
			return report
		}
	}
	report.BasisFreshness = "fresh"
	return report
}

func porcelainPath(record string) string {
	if record == "" || strings.HasPrefix(record, "# ") || strings.HasPrefix(record, "! ") {
		return ""
	}
	if strings.HasPrefix(record, "? ") {
		return strings.TrimPrefix(record, "? ")
	}
	fields := strings.Fields(record)
	if len(fields) == 0 {
		return ""
	}
	switch fields[0] {
	case "1":
		if len(fields) >= 9 {
			return fields[8]
		}
	case "2":
		if len(fields) >= 10 {
			return fields[9]
		}
	case "u":
		if len(fields) >= 11 {
			return fields[10]
		}
	}
	return "unknown"
}
