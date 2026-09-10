package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/knowledge"
	"github.com/ukyovfx/Context-Bridge/internal/registry"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
	"github.com/ukyovfx/Context-Bridge/internal/state"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

const (
	writebackReasonVerificationPending = "VERIFICATION_PENDING"
	writebackReasonAcceptedDowngraded  = "ACCEPTED_STATE_DOWNGRADED"
	writebackReasonTargetConflict      = "TARGET_CONFLICT"
	writebackReasonIdentityChanged     = "IDENTITY_CHANGED_AFTER_PLAN"
	writebackReasonWorkspaceDirty      = "WORKSPACE_DIRTY"
)

type writebackContext struct {
	Project      core.ProjectRecord
	Repository   core.RepositoryRecord
	Registered   core.WorkspaceRecord
	Probe        core.WorkspaceProbe
	Verification verificationContract
	State        state.Report
}

type writebackResult struct {
	Status       string              `json:"status"`
	Reason       string              `json:"reason,omitempty"`
	Proposal     *knowledge.Proposal `json:"proposal,omitempty"`
	Destination  string              `json:"destination,omitempty"`
	BytesWritten int                 `json:"bytes_written"`
}

func runWriteback(args []string, stdout, stderr io.Writer, _ string) error {
	if len(args) == 0 {
		return errors.New("writeback requires plan or apply")
	}
	switch args[0] {
	case "plan":
		return runWritebackPlan(args[1:], stdout, stderr)
	case "apply":
		return runWritebackApply(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown writeback command %q", args[0])
	}
}

func runWritebackPlan(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("writeback plan requires <project>")
	}
	selector := args[0]
	flags := flag.NewFlagSet("writeback plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	classValue := flags.String("class", "none", "none, active, durable_record, or accepted_state")
	summary := flags.String("summary", "", "durable semantic summary")
	status := flags.String("status", "in_progress", "active work status")
	blocker := flags.String("blocker", "", "durable blocker")
	nextAction := flags.String("next-action", "", "next safe action")
	recordKind := flags.String("kind", "decision", "decision or audit for durable_record")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("writeback plan accepts no positional arguments after <project>")
	}
	class, err := knowledge.NormalizeClass(*classValue)
	if err != nil {
		return err
	}
	kind, err := knowledge.NormalizeRecordKind(*recordKind)
	if err != nil {
		return err
	}
	if err := knowledge.ValidateText(*summary, *status, *blocker, *nextAction); err != nil {
		return err
	}
	ctx, err := resolveWritebackContext(selector)
	if err != nil {
		return emitWritebackFailure(stdout, err, *jsonOutput)
	}
	proposal := makeWritebackProposal(ctx, class, kind, *summary, *status, *blocker, *nextAction)
	if class == knowledge.ClassNone {
		proposal.EffectiveClass = knowledge.ClassNone
		proposal.Reason = "NO_DURABLE_CHANGE"
		proposal.VerificationStatus = "NOT_REQUIRED"
	} else if class == knowledge.ClassAcceptedState {
		if gateErr := acceptedStateGate(ctx); gateErr != nil {
			proposal.EffectiveClass = knowledge.ClassActive
			proposal.Reason = writebackReasonAcceptedDowngraded + ":" + gateErr.Error()
			proposal.Destination = activeDestination(proposal)
			proposal.Evidence = append(proposal.Evidence, proposal.Reason)
		} else {
			proposal.EffectiveClass = knowledge.ClassAcceptedState
			proposal.Destination = "docs/agent/CURRENT-STATE.md"
			proposal.Reason = "ACCEPTED_STATE_GATE_PASSED"
		}
	} else {
		proposal.EffectiveClass = class
		proposal.Destination = destinationFor(proposal)
	}
	if _, err := knowledge.MarshalProposal(proposal); err != nil {
		return emitWritebackFailure(stdout, err, *jsonOutput)
	}
	return emitWriteback(stdout, writebackResult{Status: "planned", Proposal: &proposal, Destination: proposal.Destination}, *jsonOutput)
}

func runWritebackApply(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("writeback apply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	proposalPath := flags.String("proposal", "", "proposal JSON path")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *proposalPath == "" {
		return errors.New("writeback apply requires --proposal <path>")
	}
	data, err := os.ReadFile(*proposalPath)
	if err != nil {
		return emitWritebackFailure(stdout, err, *jsonOutput)
	}
	proposal, err := knowledge.UnmarshalProposal(data)
	if err != nil {
		var envelope struct {
			Proposal json.RawMessage `json:"proposal"`
		}
		if json.Unmarshal(data, &envelope) == nil && len(envelope.Proposal) > 0 {
			proposal, err = knowledge.UnmarshalProposal(envelope.Proposal)
		}
	}
	if err != nil {
		return emitWritebackFailure(stdout, err, *jsonOutput)
	}
	if proposal.EffectiveClass == knowledge.ClassNone {
		return emitWriteback(stdout, writebackResult{Status: "no_write", Reason: "NO_DURABLE_CHANGE", Proposal: &proposal}, *jsonOutput)
	}
	ctx, err := resolveWritebackContext(proposal.ProjectID)
	if err != nil {
		return emitWritebackFailure(stdout, err, *jsonOutput)
	}
	if ctx.Project.ID != proposal.ProjectID || ctx.Repository.ID != proposal.RepositoryID || ctx.Registered.ID != proposal.WorkspaceID || workspace.PathKey(ctx.Probe.CanonicalPath) != workspace.PathKey(proposal.WorkspacePath) {
		return emitWritebackFailure(stdout, errors.New(writebackReasonIdentityChanged), *jsonOutput)
	}
	if writebackPreconditionFingerprint(ctx.Probe) != proposal.PreconditionFingerprint {
		return emitWritebackFailure(stdout, errors.New(writebackReasonIdentityChanged), *jsonOutput)
	}
	effective := proposal.EffectiveClass
	reason := proposal.Reason
	if effective == knowledge.ClassAcceptedState {
		if gateErr := acceptedStateGate(ctx); gateErr != nil {
			effective = knowledge.ClassActive
			reason = writebackReasonAcceptedDowngraded + ":" + gateErr.Error()
			proposal.EffectiveClass = effective
			proposal.Reason = reason
			proposal.Destination = activeDestination(proposal)
		}
	}
	content, destination, err := writebackContent(ctx, proposal, effective)
	if err != nil {
		return emitWritebackFailure(stdout, err, *jsonOutput)
	}
	written, err := applyKnowledgeFile(ctx.Probe.GitRoot, destination, content, effective == knowledge.ClassAcceptedState)
	if err != nil {
		return emitWritebackFailure(stdout, err, *jsonOutput)
	}
	resultStatus := "applied"
	if effective != proposal.EffectiveClass || reason != proposal.Reason {
		resultStatus = "downgraded"
	}
	proposal.EffectiveClass = effective
	proposal.Reason = reason
	proposal.Destination = destination
	return emitWriteback(stdout, writebackResult{Status: resultStatus, Reason: reason, Proposal: &proposal, Destination: destination, BytesWritten: written}, *jsonOutput)
}

func resolveWritebackContext(selector string) (writebackContext, error) {
	store, err := registryStore()
	if err != nil {
		return writebackContext{}, err
	}
	value, err := store.Load()
	if err != nil {
		return writebackContext{}, err
	}
	project, err := resolveHandoffProject(value, selector)
	if err != nil {
		return writebackContext{}, err
	}
	repository, err := repositoryForProject(value, project.ID)
	if err != nil {
		return writebackContext{}, err
	}
	registered, ok := canonicalWorkspace(value, repository.ID)
	if !ok {
		return writebackContext{}, errors.New(handoffReasonCanonicalWorkspaceMissing)
	}
	probe, probeStatus, probeErr := (workspace.Prober{PrimaryRemoteName: repository.PrimaryRemoteName}).ProbeWithStatus(registered.Path)
	if probeErr != nil {
		return writebackContext{}, probeErr
	}
	if probeStatus != workspace.ProbeOK {
		return writebackContext{}, errors.New(string(probeStatus))
	}
	decision := core.EvaluateGuard(guardExpected(project, repository, registered, ""), core.GuardActual{ProjectFound: true, ProjectID: project.ID, RepositoryID: repository.ID, WorkspaceID: registered.ID, Probe: probe})
	if !decision.Allowed {
		return writebackContext{}, core.WrongWorkspaceError{Reasons: decision.Reasons}
	}
	entrypoints := contextEntrypoints(probe.GitRoot)
	verification, _ := resolveVerification(probe.GitRoot, entrypoints)
	return writebackContext{Project: project, Repository: repository, Registered: registered, Probe: probe, Verification: verification, State: state.Evaluate(probe.GitRoot, workspace.CommandRunner{})}, nil
}

func makeWritebackProposal(ctx writebackContext, class, kind, summary, status, blocker, nextAction string) knowledge.Proposal {
	id := knowledge.DeterministicID(ctx.Project.ID, class, kind, summary, status, blocker, nextAction)
	return knowledge.Proposal{SchemaVersion: knowledge.SchemaVersion, ID: id, ProjectID: ctx.Project.ID, ProjectName: ctx.Project.DisplayName, RepositoryID: ctx.Repository.ID, WorkspaceID: ctx.Registered.ID, WorkspacePath: ctx.Probe.CanonicalPath, RequestedClass: class, EffectiveClass: class, RecordKind: kind, Summary: strings.TrimSpace(summary), Status: strings.TrimSpace(status), Blocker: strings.TrimSpace(blocker), NextAction: strings.TrimSpace(nextAction), BasisBranch: ctx.Probe.Branch, BasisCommit: ctx.Probe.Head, VerificationStatus: ctx.Verification.Status, VerificationCommands: append([]string(nil), ctx.Verification.RequiredCommands...), PreconditionFingerprint: writebackPreconditionFingerprint(ctx.Probe), Evidence: []string{"workspace_guard_passed", "fresh_git_probe"}}
}

func writebackPreconditionFingerprint(probe core.WorkspaceProbe) string {
	value := strings.Join([]string{probe.Fingerprint, probe.PorcelainV2, fmt.Sprint(probe.Staged), fmt.Sprint(probe.Unstaged), fmt.Sprint(probe.Untracked)}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func acceptedStateGate(ctx writebackContext) error {
	if ctx.Probe.Detached || ctx.Probe.Branch == "" || ctx.Probe.Branch != ctx.Repository.CanonicalBranch {
		return errors.New("BRANCH_NOT_ACCEPTED")
	}
	if ctx.Probe.Head == "" {
		return errors.New("HEAD_UNAVAILABLE")
	}
	if len(ctx.Probe.EvidenceErrors) > 0 {
		return errors.New("PROBE_EVIDENCE_INCOMPLETE")
	}
	if ctx.Probe.Staged || ctx.Probe.Unstaged || ctx.Probe.Untracked {
		return errors.New(writebackReasonWorkspaceDirty)
	}
	if ctx.Verification.Status != "RESOLVED" {
		return errors.New("NO_VERIFICATION_CONTRACT")
	}
	if ctx.State.ContentIntegrity != "ok" || ctx.State.BasisValidity != "valid" || ctx.State.BasisFreshness != "fresh" {
		return errors.New("CURRENT_STATE_NOT_FRESH")
	}
	if err := runVerificationCommands(ctx.Probe.GitRoot, ctx.Verification.RequiredCommands); err != nil {
		return err
	}
	return nil
}

func runVerificationCommands(root string, commands []string) error {
	for _, command := range commands {
		lower := strings.ToLower(command)
		for _, forbidden := range []string{"fetch", "pull", "push", "checkout", "reset", "clean", "gc", "prune", "repair", "update-index"} {
			if strings.Contains(lower, forbidden) {
				return errors.New("verification command contains a forbidden Git mutation: " + forbidden)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", command)
		}
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		err := cmd.Run()
		cancel()
		if err != nil {
			return errors.New("VERIFICATION_COMMAND_FAILED")
		}
	}
	return nil
}

func destinationFor(proposal knowledge.Proposal) string {
	switch proposal.EffectiveClass {
	case knowledge.ClassActive:
		return activeDestination(proposal)
	case knowledge.ClassDurableRecord:
		directory := "audits"
		if proposal.RecordKind == knowledge.RecordDecision {
			directory = "decisions"
		}
		return "docs/agent/" + directory + "/" + knowledge.Slug(proposal.Summary) + "-" + proposal.ID + ".md"
	case knowledge.ClassAcceptedState:
		return "docs/agent/CURRENT-STATE.md"
	default:
		return ""
	}
}

func activeDestination(proposal knowledge.Proposal) string {
	return "docs/agent/plans/active/" + knowledge.Slug(proposal.Summary) + "-" + proposal.ID + ".md"
}

func writebackContent(ctx writebackContext, proposal knowledge.Proposal, class string) ([]byte, string, error) {
	proposal.EffectiveClass = class
	destination := destinationFor(proposal)
	if class == knowledge.ClassAcceptedState {
		path, err := safety.JoinWithin(ctx.Probe.GitRoot, destination)
		if err != nil {
			return nil, "", err
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
		return updateCurrentState(old, proposal)
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "---\ncontextbridge_writeback_schema: 1\nwriteback_id: %s\nclass: %s\nproject_id: %s\nbasis_branch: %s\nbasis_commit: %s\nverification_status: %s\n---\n", proposal.ID, class, proposal.ProjectID, proposal.BasisBranch, proposal.BasisCommit, proposal.VerificationStatus)
	fmt.Fprintf(&builder, "# %s\n\n<!-- contextbridge-writeback:%s -->\n\n%s\n", proposal.Summary, proposal.ID, proposal.Summary)
	if proposal.Status != "" {
		fmt.Fprintf(&builder, "\n## Status\n\n%s\n", proposal.Status)
	}
	if proposal.Blocker != "" {
		fmt.Fprintf(&builder, "\n## Blocker\n\n%s\n", proposal.Blocker)
	}
	if proposal.NextAction != "" {
		fmt.Fprintf(&builder, "\n## Next safe action\n\n%s\n", proposal.NextAction)
	}
	return []byte(builder.String()), destination, nil
}

func updateCurrentState(old []byte, proposal knowledge.Proposal) ([]byte, string, error) {
	marker := "<!-- contextbridge-writeback:" + proposal.ID + " -->"
	if bytes.Contains(old, []byte(marker)) {
		return old, "docs/agent/CURRENT-STATE.md", nil
	}
	text := strings.ReplaceAll(string(old), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return nil, "", errors.New("CURRENT-STATE front matter is missing")
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			end = index
			break
		}
	}
	if end < 0 {
		return nil, "", errors.New("CURRENT-STATE front matter is invalid")
	}
	seenBranch, seenCommit, seenDate := false, false, false
	for index := 1; index < end; index++ {
		key := strings.TrimSpace(strings.SplitN(lines[index], ":", 2)[0])
		switch key {
		case "basis_branch":
			lines[index] = "basis_branch: " + proposal.BasisBranch
			seenBranch = true
		case "basis_commit":
			lines[index] = "basis_commit: " + proposal.BasisCommit
			seenCommit = true
		case "basis_date":
			lines[index] = "basis_date: " + time.Now().UTC().Format(time.RFC3339)
			seenDate = true
		}
	}
	if !seenBranch {
		lines = append(lines[:end], append([]string{"basis_branch: " + proposal.BasisBranch}, lines[end:]...)...)
		end++
	}
	if !seenCommit {
		lines = append(lines[:end], append([]string{"basis_commit: " + proposal.BasisCommit}, lines[end:]...)...)
		end++
	}
	if !seenDate {
		lines = append(lines[:end], append([]string{"basis_date: " + time.Now().UTC().Format(time.RFC3339)}, lines[end:]...)...)
	}
	updated := strings.Join(lines, "\n")
	updated += fmt.Sprintf("\n\n## Accepted write-back\n\n%s\n\n%s\n", marker, proposal.Summary)
	return []byte(updated), "docs/agent/CURRENT-STATE.md", nil
}

func applyKnowledgeFile(root, relative string, content []byte, replace bool) (int, error) {
	path, err := safety.JoinWithin(root, relative)
	if err != nil {
		return 0, err
	}
	if err := rejectReparseComponents(root, path); err != nil {
		return 0, err
	}
	if replace {
		if err := registry.AtomicReplaceFile(path, content, 0o644); err != nil {
			return 0, err
		}
		return len(content), nil
	}
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, content) {
			return 0, nil
		}
		return 0, errors.New(writebackReasonTargetConflict)
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	if err := rejectReparseComponents(root, path); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if writeErr != nil {
		return 0, writeErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	return len(content), nil
}

func rejectReparseComponents(root, target string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		linked, statErr := safety.IsLinkOrReparse(current)
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if linked {
			return errors.New("safety abort: knowledge destination is a reparse point")
		}
	}
	return nil
}

func emitWriteback(stdout io.Writer, result writebackResult, jsonOutput bool) error {
	if jsonOutput {
		data, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintln(stdout, `{"status":"failed","reason":"JSON_ENCODE_FAILED"}`)
			return err
		}
		fmt.Fprintln(stdout, string(data))
		return nil
	}
	fmt.Fprintf(stdout, "Write-back: %s\n", strings.ToUpper(result.Status))
	if result.Proposal != nil {
		fmt.Fprintf(stdout, "Class: %s\nProject: %s\nDestination: %s\nReason: %s\n", result.Proposal.EffectiveClass, result.Proposal.ProjectName, result.Destination, result.Reason)
	}
	if result.BytesWritten > 0 {
		fmt.Fprintf(stdout, "Bytes written: %d\n", result.BytesWritten)
	}
	return nil
}

func emitWritebackFailure(stdout io.Writer, err error, jsonOutput bool) error {
	reason := publicWritebackReason(err)
	if jsonOutput {
		fmt.Fprintf(stdout, "{\"status\":\"failed\",\"reason\":%q}\n", reason)
	}
	return err
}

func publicWritebackReason(err error) string {
	message := err.Error()
	known := []string{writebackReasonIdentityChanged, writebackReasonTargetConflict, writebackReasonWorkspaceDirty, writebackReasonAcceptedDowngraded, writebackReasonVerificationPending, "BRANCH_NOT_ACCEPTED", "HEAD_UNAVAILABLE", "PROBE_EVIDENCE_INCOMPLETE", "NO_VERIFICATION_CONTRACT", "CURRENT_STATE_NOT_FRESH", "VERIFICATION_COMMAND_FAILED"}
	for _, code := range known {
		if strings.Contains(message, code) {
			return code
		}
	}
	if strings.Contains(message, "proposal JSON") || strings.Contains(message, "proposal schema") || strings.Contains(message, "proposal identity") || strings.Contains(message, "summary") || strings.Contains(message, "secret") {
		return "INVALID_PROPOSAL"
	}
	return "WRITEBACK_FAILED"
}

func runKnowledgeDoctor(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("knowledge doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectPath := flags.String("project", "", "project path; defaults to the current directory")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("knowledge doctor accepts no positional arguments")
	}
	root := *projectPath
	if root == "" {
		root, _ = os.Getwd()
	}
	root, err := workspace.CanonicalPath(root)
	if err != nil {
		return emitKnowledgeDoctorFailure(stdout, err, *jsonOutput)
	}
	issues := make([]string, 0)
	for _, relative := range []string{"AGENTS.md", "docs/agent/START-HERE.md", "docs/agent/CURRENT-STATE.md", "docs/agent/plans/active", "docs/agent/decisions", "docs/agent/audits"} {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); statErr != nil && os.IsNotExist(statErr) {
			issues = append(issues, "MISSING:"+relative)
		}
	}
	report := state.Evaluate(root, workspace.CommandRunner{})
	if report.ContentIntegrity != "ok" || report.BasisValidity != "valid" || report.BasisFreshness != "fresh" {
		issues = append(issues, "CURRENT_STATE_PROVENANCE_UNVERIFIED")
	}
	ids := map[string]string{}
	for _, directory := range []string{"docs/agent/plans/active", "docs/agent/decisions", "docs/agent/audits"} {
		path := filepath.Join(root, filepath.FromSlash(directory))
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			filePath := filepath.Join(path, entry.Name())
			info, statErr := entry.Info()
			if statErr == nil && info.Size() > 256*1024 {
				issues = append(issues, "SIZE_LIMIT_EXCEEDED:"+filepath.ToSlash(filepath.Join(directory, entry.Name())))
			}
			data, readErr := os.ReadFile(filePath)
			if readErr != nil {
				continue
			}
			relativePath := filepath.ToSlash(filepath.Join(directory, entry.Name()))
			id := metadataValue(string(data), "writeback_id")
			if id == "" {
				issues = append(issues, "MISSING_WRITEBACK_ID:"+relativePath)
				continue
			}
			if metadataValue(string(data), "contextbridge_writeback_schema") != "1" {
				issues = append(issues, "INVALID_WRITEBACK_SCHEMA:"+relativePath)
			}
			if class, classErr := knowledge.NormalizeClass(metadataValue(string(data), "class")); classErr != nil || class == knowledge.ClassNone {
				issues = append(issues, "INVALID_WRITEBACK_CLASS:"+relativePath)
			}
			if previous, exists := ids[id]; exists {
				issues = append(issues, "DUPLICATE_WRITEBACK_ID:"+id+":"+previous)
			} else {
				ids[id] = relativePath
			}
		}
	}
	status := "PASS"
	if len(issues) > 0 {
		status = "WARNING"
	}
	result := struct {
		Status        string       `json:"status"`
		ProjectRoot   string       `json:"project_root"`
		Issues        []string     `json:"issues"`
		AcceptedState state.Report `json:"accepted_state"`
	}{status, root, issues, report}
	if *jsonOutput {
		data, _ := json.Marshal(result)
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "Knowledge: %s\nProject: %s\nIssues: %d\n", status, root, len(issues))
		for _, issue := range issues {
			fmt.Fprintf(stdout, "- %s\n", issue)
		}
	}
	return nil
}

func metadataValue(content, key string) string {
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func emitKnowledgeDoctorFailure(stdout io.Writer, err error, jsonOutput bool) error {
	if jsonOutput {
		fmt.Fprintf(stdout, "{\"status\":\"FAILED\",\"issues\":[%q]}\n", err.Error())
	}
	return err
}
