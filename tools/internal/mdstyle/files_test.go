package mdstyle_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/mdstyle"
)

func writeMD(t *testing.T, root, rel, content string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestCheckFilesReportsEachOffendingFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bad := writeMD(t, root, "docs/a.md", "wrapped\nparagraph\n")
	writeMD(t, root, "docs/b.md", "one line\n")

	got, err := mdstyle.CheckFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != bad || len(got[0].Lines) != 1 || got[0].Lines[0] != 2 {
		t.Errorf("CheckFiles = %+v, want only %s line 2", got, bad)
	}
}

func TestFixRewritesOnlyWhatChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bad := writeMD(t, root, "a.md", "wrapped\nparagraph\n")
	good := writeMD(t, root, "b.md", "fine\n")

	changed, err := mdstyle.Fix(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != bad {
		t.Fatalf("Fix changed %v, want only %s", changed, bad)
	}
	if data, _ := os.ReadFile(bad); string(data) != "wrapped paragraph\n" {
		t.Errorf("a.md = %q", data)
	}
	if data, _ := os.ReadFile(good); string(data) != "fine\n" {
		t.Errorf("b.md must be untouched, got %q", data)
	}
	if left, _ := mdstyle.CheckFiles(root); len(left) != 0 {
		t.Errorf("after Fix nothing should remain: %+v", left)
	}
}
