// SPDX-License-Identifier: Apache-2.0

package main

import "os"

func main() {
	if err := Execute(); err != nil {
		os.Exit(exitCode(err))
	}
}
