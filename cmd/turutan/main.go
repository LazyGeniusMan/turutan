// SPDX-License-Identifier: Apache-2.0

// Command turutan manages project-template lifecycle: bootstrap,
// check-update, diff and update against versioned template sources.
package main

import "os"

func main() {
	if err := Execute(); err != nil {
		os.Exit(exitCode(err))
	}
}
