// SPDX-License-Identifier: Apache-2.0

// Package scaffold orchestrates the bootstrap/update/diff/check-update
// flows on top of the template, git, filter and config packages.
// Orchestration logic arrives in M1+; M0 holds the option surface.
package scaffold

import (
	"fmt"
	"io"
	"os"
)

// ConflictMode selects how unmergeable hunks are recorded.
type ConflictMode string

// Conflict modes (spec §7).
const (
	ConflictInline ConflictMode = "inline"
	ConflictRej    ConflictMode = "rej"
)

// Validate rejects unknown conflict modes.
func (c ConflictMode) Validate() error {
	switch c {
	case "", ConflictInline, ConflictRej:
		return nil
	default:
		return fmt.Errorf("unknown conflict mode %q (want inline or rej)", string(c))
	}
}

// Options carries run-wide knobs shared by all flows.
type Options struct {
	NonInteractive bool
	Force          bool
	Conflict       ConflictMode
	Ref            string
	// Subpath overrides the URI //subpath at bootstrap.
	Subpath string
	// AnswersFile loads answers from a YAML/JSON file (reproducible CI).
	AnswersFile string
	// Defaults accepts template defaults non-interactively.
	Defaults bool
	// AllowHooks consents to template-declared hooks; without it,
	// --non-interactive runs refuse hook-bearing templates.
	AllowHooks bool
	// Skip lists template-relative paths kept as-is, recorded in state.
	Skip []string
	// Engine is the bare CLI version (for example "0.1.0" or "dev")
	// used for the min-engine gate and recorded in state.
	Engine string
	// Verbose enables per-run diagnostics on Stderr (resolved refs,
	// fetch decisions, file counts); quiet by default.
	Verbose bool
	// Stdin, Stdout and Stderr default to the OS streams when nil.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// vlogf writes a verbose diagnostic to stderr when verbose is set. A nil
// stderr falls back to os.Stderr; callers pass their configured stream so
// tests can capture the output. Quiet runs emit nothing.
func vlogf(stderr io.Writer, verbose bool, format string, args ...any) {
	if !verbose {
		return
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	fmt.Fprintf(stderr, "turutan: "+format+"\n", args...)
}
