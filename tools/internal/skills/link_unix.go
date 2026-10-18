//go:build !windows

package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// makeLink creates a relative symlink, so the repository can move.
func makeLink(_ context.Context, source, link string) error {
	target, err := filepath.Rel(filepath.Dir(link), source)
	if err != nil {
		return fmt.Errorf("relate %s to %s: %w", link, source, err)
	}
	if err = os.Symlink(target, link); err != nil {
		return fmt.Errorf("link %s: %w", LinkDir, err)
	}
	return nil
}
