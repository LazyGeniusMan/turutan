// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"fmt"
	"io"
	"os"
)

type ConflictMode string

const (
	ConflictInline ConflictMode = "inline"
	ConflictRej    ConflictMode = "rej"
)

func (c ConflictMode) Validate() error {
	switch c {
	case "", ConflictInline, ConflictRej:
		return nil
	default:
		return fmt.Errorf("unknown conflict mode %q (want inline or rej)", string(c))
	}
}

type Options struct {
	NonInteractive bool
	Force          bool
	Conflict       ConflictMode
	Ref            string
	Subpath        string
	AnswersFile    string
	Defaults       bool
	AllowHooks     bool
	Skip           []string
	Engine         string
	Verbose        bool
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
}

func vlogf(stderr io.Writer, verbose bool, format string, args ...any) {
	if !verbose {
		return
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	fmt.Fprintf(stderr, "turutan: "+format+"\n", args...)
}
