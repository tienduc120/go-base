package guard

import (
	"path/filepath"
	"strings"
)

// CheckEdit blocks changing an existing protected file. target may be absolute or relative to cwd; exists reports
// whether a file is already on disk, because creating a new one is always allowed.
func CheckEdit(root, cwd, target string, exists func(string) bool) Decision {
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Decision{}
	}
	rel = filepath.ToSlash(rel)

	for _, rule := range protectedRules() {
		if rule.matches(rel) && exists(abs) {
			return block("%s is %s. Fix the cause instead of changing it; adding new files is allowed. %s",
				rel, rule.what, approvalHint())
		}
	}
	return Decision{}
}
