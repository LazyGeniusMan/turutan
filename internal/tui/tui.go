// SPDX-License-Identifier: Apache-2.0

// Package tui renders interactive diff views. The Bubbletea UI and the
// non-interactive stdout fallback arrive in M2; M0 holds the option surface
// so scaffold can depend on display without importing UI toolkits.
package tui

import "io"

// Options selects the presentation mode. NoColor guarantees the UI
// emits no ANSI escape sequences; the review view is plain text either
// way, and the flag guards any future styling.
type Options struct {
	NoColor        bool
	NonInteractive bool
	Stdout         io.Writer
}
