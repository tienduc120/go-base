package mdstyle_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/mdstyle"
)

func TestUnwrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"a wrapped paragraph becomes one line",
			"The first part of a sentence\ncontinues here and\nends here.\n",
			"The first part of a sentence continues here and ends here.\n",
		},
		{
			"paragraphs stay separate",
			"One paragraph\nwrapped.\n\nAnother\nwrapped.\n",
			"One paragraph wrapped.\n\nAnother wrapped.\n",
		},
		{
			"a list item's continuation lines join the item",
			"- first item that is long\n  and keeps going\n- second item\n",
			"- first item that is long and keeps going\n- second item\n",
		},
		{
			"numbered and nested items are separate lines",
			"1. one\n2. two\n   - nested a\n   - nested b\n",
			"1. one\n2. two\n   - nested a\n   - nested b\n",
		},
		{
			"headings never join their neighbours",
			"# Title\nbody text\n## Next\nmore\n",
			"# Title\nbody text\n## Next\nmore\n",
		},
		{
			"table rows are left alone",
			"| a | b |\n|---|---|\n| 1 | 2 |\n",
			"| a | b |\n|---|---|\n| 1 | 2 |\n",
		},
		{
			"fenced code is untouched, even when it looks like prose",
			"Intro text.\n\n```sh\nthis looks like\nprose but is code\n```\n\nAfter.\n",
			"Intro text.\n\n```sh\nthis looks like\nprose but is code\n```\n\nAfter.\n",
		},
		{
			"a quote and html are left alone",
			"> quoted line one\n> quoted line two\n\n<details>\n<summary>x</summary>\n",
			"> quoted line one\n> quoted line two\n\n<details>\n<summary>x</summary>\n",
		},
		{
			"a deliberate hard break survives",
			"line one  \nline two\n",
			"line one  \nline two\n",
		},
		{
			"a trailing backslash break survives",
			"line one\\\nline two\n",
			"line one\\\nline two\n",
		},
		{
			"front matter is copied verbatim",
			"---\nname: x\ndescription: y\n---\n\nBody wrapped\nhere.\n",
			"---\nname: x\ndescription: y\n---\n\nBody wrapped here.\n",
		},
		{"blank lines and the final newline are kept", "a\n\n\nb\n", "a\n\n\nb\n"},
		{"no final newline stays that way", "a\nb", "a b"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mdstyle.Unwrap(tc.in); got != tc.want {
				t.Errorf("Unwrap(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestUnwrapIsIdempotent(t *testing.T) {
	t.Parallel()

	in := "Para one\nwrapped.\n\n- item\n  more\n\n```\ncode\nlines\n```\n\n| a |\n|---|\n"
	once := mdstyle.Unwrap(in)
	if twice := mdstyle.Unwrap(once); twice != once {
		t.Errorf("not idempotent:\n once  %q\n twice %q", once, twice)
	}
}

func TestCheckReportsTheContinuationLines(t *testing.T) {
	t.Parallel()

	src := "# Title\n\nFirst line of a paragraph\nsecond line\nthird line\n\n- item\n  continues\n"
	got := mdstyle.Check(src)

	if want := []int{4, 5, 8}; !slices.Equal(got, want) {
		t.Errorf("Check = %v, want %v", got, want)
	}
}

func TestCheckPassesCleanText(t *testing.T) {
	t.Parallel()

	if got := mdstyle.Check("# T\n\nOne long line.\n\n- a\n- b\n"); len(got) != 0 {
		t.Errorf("Check = %v, want none", got)
	}
}

func TestFilesSkipsWhatIsNotOurs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := []string{
		"README.md", "docs/adr/0001.md", ".local/plan.md", ".agentctl/logs/x.md", "LICENSES/upstream.md",
		"testdata/clean/x.md", "node_modules/pkg/README.md", "notes.txt",
	}
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := mdstyle.Files(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "README.md"), filepath.Join(root, "docs", "adr", "0001.md")}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Files = %v, want %v", got, want)
	}
}
