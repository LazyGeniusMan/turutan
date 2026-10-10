// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestResolveStreams(t *testing.T) {
	t.Parallel()
	t.Run("explicit streams pass through", func(t *testing.T) {
		t.Parallel()
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		stdin := bytes.NewReader(nil)
		gotOut, gotErr, gotIn := resolveStreams(stdout, stderr, stdin)
		if gotOut != io.Writer(stdout) || gotErr != io.Writer(stderr) || gotIn != io.Reader(stdin) {
			t.Error("resolveStreams did not pass explicit streams through")
		}
	})
	t.Run("nils fall back to OS streams", func(t *testing.T) {
		t.Parallel()
		gotOut, gotErr, gotIn := resolveStreams(nil, nil, nil)
		if gotOut != io.Writer(os.Stdout) || gotErr != io.Writer(os.Stderr) || gotIn != io.Reader(os.Stdin) {
			t.Error("resolveStreams(nil, nil, nil) did not fall back to OS streams")
		}
	})
	t.Run("mixed keeps explicit and defaults rest", func(t *testing.T) {
		t.Parallel()
		stderr := &bytes.Buffer{}
		gotOut, gotErr, gotIn := resolveStreams(nil, stderr, nil)
		if gotOut != io.Writer(os.Stdout) || gotErr != io.Writer(stderr) || gotIn != io.Reader(os.Stdin) {
			t.Error("resolveStreams did not mix explicit stderr with OS defaults")
		}
	})
}
