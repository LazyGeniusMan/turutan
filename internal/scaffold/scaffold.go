// SPDX-License-Identifier: Apache-2.0

// Package scaffold orchestrates the bootstrap/update/diff/check-update
// flows on top of the template, git, filter and config packages.
// Orchestration logic arrives in M1+; M0 holds the option surface.
package scaffold

// ConflictMode selects how unmergeable hunks are recorded.
type ConflictMode string

// Conflict modes (spec §7).
const (
	ConflictInline ConflictMode = "inline"
	ConflictRej    ConflictMode = "rej"
)

// Options carries run-wide knobs shared by all flows.
type Options struct {
	NonInteractive bool
	Force          bool
	Conflict       ConflictMode
	Ref            string
}
