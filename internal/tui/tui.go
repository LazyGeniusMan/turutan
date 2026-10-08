// SPDX-License-Identifier: Apache-2.0

// Package tui renders interactive diff views. The Bubbletea UI and the
// non-interactive stdout fallback arrive in M2; M0 holds the option surface
// so scaffold can depend on display without importing UI toolkits.
package tui

import "io"

// Options selects the presentation mode.
type Options struct {
	NoColor        bool
	NonInteractive bool
	Stdout         io.Writer
}
