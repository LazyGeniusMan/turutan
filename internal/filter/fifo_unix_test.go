// SPDX-License-Identifier: Apache-2.0

//go:build unix

package filter

import "syscall"

func makeFifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}
