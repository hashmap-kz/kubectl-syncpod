//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Without stdin
// out, err := runCmd("echo", "hello world")
// fmt.Printf("output: %q, err: %v\n", out, err)
func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)

	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return out.String(), nil
}

// With stdin
// out, err = runCmdWithStdin("hello from stdin\n", "cat")
// fmt.Printf("output: %q, err: %v\n", out, err)
func runCmdWithStdin(stdin string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)

	cmd.Stdin = strings.NewReader(stdin)

	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return out.String(), nil
}
