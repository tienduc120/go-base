package bad

import "time"

type Invoice struct {
	ID     string // want `exported field ID on domain type Invoice`
	amount int64
}

type Tagged struct {
	name string `json:"name"` // want `struct tag in a domain type`
}

type Embedded struct {
	time.Time // want `exported field Time on domain type Embedded`
}

//agent:allow-exported-fields
type NoReason struct { // want `directive needs a reason`
	Value int
}

func Now() time.Time {
	return time.Now() // want `domain reads the clock`
}

func Age(since time.Time) time.Duration {
	return time.Since(since) // want `domain reads the clock`
}
