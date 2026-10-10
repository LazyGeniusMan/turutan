// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"io"
	"os"
)

func resolveStreams(
	stdout io.Writer,
	stderr io.Writer,
	stdin io.Reader,
) (io.Writer, io.Writer, io.Reader) {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if stdin == nil {
		stdin = os.Stdin
	}
	return stdout, stderr, stdin
}
