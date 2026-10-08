// SPDX-License-Identifier: Apache-2.0

package template

import "errors"

// errNotImplemented reports M0 stub boundaries; replaced as M1+ lands.
func errNotImplemented() error {
	return errors.New("not implemented (M1+)")
}
