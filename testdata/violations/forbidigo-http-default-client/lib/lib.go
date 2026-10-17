// Package lib is a violation fixture.
package lib

import (
	"net/http"
)

func Do(req *http.Request) (*http.Response, error) { return http.DefaultClient.Do(req) }
