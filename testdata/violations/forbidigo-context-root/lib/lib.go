// Package lib is a violation fixture.
package lib

import (
	stdctx "context"
)

func Root() stdctx.Context { return stdctx.Background() }
