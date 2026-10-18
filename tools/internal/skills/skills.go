// Package skills keeps the agent skills in one place and links Claude Code to them.
//
// Skills live in .agents/skills, which any agent can read. Claude Code looks in .claude/skills, so that path is a link
// to the first, made by `agentctl skills link` and never committed: a committed symlink arrives as a plain file on a
// Windows checkout without symlink rights, and Claude Code would silently lose every skill.
package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// SourceDir is where skills are written and committed.
	SourceDir = ".agents/skills"
	// LinkDir is where Claude Code reads them from.
	LinkDir = ".claude/skills"

	dirMode = 0o750
)

const fix = "run `agentctl skills link`"

func sourcePath(root string) string { return filepath.Join(root, filepath.FromSlash(SourceDir)) }

func linkPath(root string) string { return filepath.Join(root, filepath.FromSlash(LinkDir)) }

// isLink reports a symbolic link or, on Windows, a junction, which Go reports as irregular.
func isLink(info os.FileInfo) bool {
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

// Check reports nil when LinkDir leads to SourceDir.
func Check(root string) error {
	source, link := sourcePath(root), linkPath(root)
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return fmt.Errorf("%s does not exist: skills are written there", SourceDir)
	}

	info, err := os.Lstat(link)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%s is missing, so Claude Code sees no skills: %s", LinkDir, fix)
	case err != nil:
		return fmt.Errorf("check %s: %w", LinkDir, err)
	case !isLink(info):
		return fmt.Errorf("%s is a real directory, not a link to %s: move its skills into %s, remove it, and %s",
			LinkDir, SourceDir, SourceDir, fix)
	}

	target, err := os.Stat(link)
	if err != nil {
		return fmt.Errorf("%s is a dangling link: %s", LinkDir, fix)
	}
	want, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("check %s: %w", SourceDir, err)
	}
	if !os.SameFile(target, want) {
		return fmt.Errorf("%s leads somewhere other than %s: %s", LinkDir, SourceDir, fix)
	}
	return nil
}

// Link makes LinkDir lead to SourceDir. It does nothing when it already does, replaces a stale link, and refuses to
// touch a real directory, which may hold skills that exist nowhere else.
func Link(ctx context.Context, root string) error {
	source, link := sourcePath(root), linkPath(root)
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return fmt.Errorf("%s does not exist: there is nothing to link to", SourceDir)
	}
	if Check(root) == nil {
		return nil
	}

	if info, err := os.Lstat(link); err == nil {
		if !isLink(info) {
			return fmt.Errorf("%s is a real directory: move its skills into %s and remove it first, "+
				"since nothing is deleted to make room for a link", LinkDir, SourceDir)
		}
		if err = os.Remove(link); err != nil {
			return fmt.Errorf("remove the stale link %s: %w", LinkDir, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(link), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(LinkDir), err)
	}
	return makeLink(ctx, source, link)
}
