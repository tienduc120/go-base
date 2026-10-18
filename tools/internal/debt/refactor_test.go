package debt_test

import (
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/debt"
)

const additionsOnly = `diff --git a/pkg/a_test.go b/pkg/a_test.go
index 111..222 100644
--- a/pkg/a_test.go
+++ b/pkg/a_test.go
@@ -10,0 +11,4 @@ func TestA(t *testing.T) {
+func TestPinsTheOddBranch(t *testing.T) {
+	t.Parallel()
+	// a new characterization test
+}
`

const editedAssertion = `diff --git a/pkg/a_test.go b/pkg/a_test.go
index 111..222 100644
--- a/pkg/a_test.go
+++ b/pkg/a_test.go
@@ -12 +12 @@ func TestA(t *testing.T) {
-	if got != 3 {
+	if got != 4 {
`

const deletedFile = `diff --git a/pkg/old_test.go b/pkg/old_test.go
deleted file mode 100644
index 111..000
--- a/pkg/old_test.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package pkg
-
-func TestOld(t *testing.T) {}
`

func TestRefactorMayOnlyAddTests(t *testing.T) {
	t.Parallel()

	if got := debt.RefactorViolations(additionsOnly); len(got) != 0 {
		t.Errorf("new tests are exactly what a refactor needs: %v", got)
	}
}

func TestRefactorMayNotEditAnExistingAssertion(t *testing.T) {
	t.Parallel()

	got := debt.RefactorViolations(editedAssertion)
	if len(got) != 1 || !strings.Contains(got[0], "pkg/a_test.go") || !strings.Contains(got[0], "got != 3") {
		t.Errorf("an edited expectation means behaviour changed: %v", got)
	}
}

func TestRefactorMayNotDeleteATestFile(t *testing.T) {
	t.Parallel()

	got := debt.RefactorViolations(deletedFile)
	if len(got) == 0 || !strings.Contains(got[0], "pkg/old_test.go") {
		t.Errorf("deleting tests is not refactoring: %v", got)
	}
}

func TestRefactorIgnoresBlankAndCommentOnlyRemovals(t *testing.T) {
	t.Parallel()

	diff := "diff --git a/p/a_test.go b/p/a_test.go\n--- a/p/a_test.go\n+++ b/p/a_test.go\n@@ -3,2 +3,0 @@\n-\n-\t// an old note\n"
	if got := debt.RefactorViolations(diff); len(got) != 0 {
		t.Errorf("blank lines and comments carry no behaviour: %v", got)
	}
}

func TestRefactorIgnoresNonTestFiles(t *testing.T) {
	t.Parallel()

	diff := "diff --git a/p/a.go b/p/a.go\n--- a/p/a.go\n+++ b/p/a.go\n@@ -3 +3 @@\n-\treturn 1\n+\treturn 2\n"
	if got := debt.RefactorViolations(diff); len(got) != 0 {
		t.Errorf("production code is what a refactor is allowed to change: %v", got)
	}
}

func TestRefactorViolationsAreBoundedAndEmptyDiffIsFine(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	sb.WriteString("diff --git a/p/a_test.go b/p/a_test.go\n--- a/p/a_test.go\n+++ b/p/a_test.go\n@@ -1,30 +0,0 @@\n")
	for range 30 {
		sb.WriteString("-\tassertThing()\n")
	}
	if got := debt.RefactorViolations(sb.String()); len(got) > 11 {
		t.Errorf("got %d lines; a report must stay short (10 and a summary)", len(got))
	}
	if got := debt.RefactorViolations(""); len(got) != 0 {
		t.Errorf("empty diff: %v", got)
	}
}
