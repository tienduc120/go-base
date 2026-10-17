// Package lib is a violation fixture.
package lib

import (
	"context"
	"net/http"
)

func Status(ctx context.Context, c *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	return resp.StatusCode, nil
}
