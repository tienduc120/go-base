// Package lib is a violation fixture.
package lib

import (
	"os"
)

func Cleanup() {
	os.Remove("tmp") //nolint:errcheck
}
