// Package lib is a violation fixture.
package lib

import (
	"net/http"
)

func Fetch(url string) (*http.Response, error) { return http.Get(url) }
