// Package lib is a violation fixture.
package lib

import (
	"net/http"
)

func Build(url string) (*http.Request, error) {
	return http.NewRequest(http.MethodGet, url, nil)
}
