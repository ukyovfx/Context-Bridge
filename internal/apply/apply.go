package apply

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
)

type Runner interface {
	Run(directory, name string, args ...string) error
}

type CommandRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

func (r CommandRunner) Run(directory, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = directory
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

type Applier struct {
	Runner Runner
}

func (a Applier) Apply(plan core.Plan) error {
	if a.Runner == nil {
		return errors.New("runner is required")
	}
	target := plan.Target()
	exists, err := safety.TargetExists(target)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("safety abort: target appeared after planning")
	}
	for _, op := range plan.Operations() {
		if err := a.applyOperation(target, op); err != nil {
			return fmt.Errorf("apply %s: %w", op.Kind, err)
		}
	}
	return nil
}

func (a Applier) applyOperation(target string, op core.Operation) error {
	switch op.Kind {
	case core.CreateDirectory:
		path, err := safety.JoinWithin(target, op.Path)
		if err != nil {
			return err
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			return err
		}
		return nil
	case core.WriteFile:
		path, err := safety.JoinWithin(target, op.Path)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		_, writeErr := file.WriteString(op.Content)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	case core.InitializeGit:
		return a.Runner.Run(target, "git", "init", "-b", "main")
	case core.StageGitFiles:
		return a.Runner.Run(target, "git", "add", "--all")
	case core.CommitGitFiles:
		if len(op.Args) != 1 {
			return errors.New("invalid commit operation")
		}
		return a.Runner.Run(target, "git", "commit", "-m", op.Args[0])
	case core.CreatePrivateAndPush:
		if len(op.Args) != 1 {
			return errors.New("invalid GitHub operation")
		}
		return a.Runner.Run(target, "gh", "repo", "create", op.Args[0], "--private", "--source", ".", "--remote", "origin", "--push")
	default:
		return fmt.Errorf("unknown operation %q", op.Kind)
	}
}
