// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package filter

import "errors"

func makeFifo(_ string) error {
	return errors.New("fifos unavailable on this platform")
}
