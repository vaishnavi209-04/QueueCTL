package core

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type Executor struct {
	Timeout time.Duration
}

type ExecutionResult struct {
	Error  error
	Output string
}

func NewExecutor(timeout time.Duration) *Executor {
	return &Executor{Timeout: timeout}
}

func (e *Executor) Run(command string) ExecutionResult {
	ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.CombinedOutput()

	res := ExecutionResult{
		Output: string(out),
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			res.Error = fmt.Errorf("timeout exceeded: %s", e.Timeout)
		} else {
			res.Error = err
		}
	}

	return res
}
