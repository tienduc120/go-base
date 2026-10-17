package barego_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tienduc120/go-base/tools/analyzers/barego"
)

func TestBareGo(t *testing.T) {
	t.Parallel()

	// "bare" must be reported; the async package is the one allowed place; _test.go files are exempt.
	analysistest.Run(t, analysistest.TestData(), barego.New(), "bare", "example.com/x/platform/async")
}
