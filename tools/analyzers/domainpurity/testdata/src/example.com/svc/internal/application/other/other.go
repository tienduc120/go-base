package other

import "time"

type Command struct {
	ID string `json:"id"`
}

func Now() time.Time { return time.Now() }
