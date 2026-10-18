package skills

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// makeLink creates a directory junction, which needs no administrator rights or Developer Mode, unlike a symlink.
func makeLink(ctx context.Context, source, link string) error {
	out, err := exec.CommandContext(ctx, "cmd", "/c", "mklink", "/J", link, source).CombinedOutput()
	if err != nil {
		return fmt.Errorf("link %s: %w: %s", LinkDir, err, strings.TrimSpace(string(out)))
	}
	return nil
}
