// Package clock provides the system time behind an interface so business code never reads the clock itself.
package clock

import "time"

type System struct{}

func (System) Now() time.Time { return time.Now() }
