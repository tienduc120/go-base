package txboundary_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tienduc120/go-base/tools/analyzers/txboundary"
)

func TestTxBoundary(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), txboundary.New(),
		"example.com/svc/internal/application/good",
		"example.com/svc/internal/application/bad",
		"example.com/svc/internal/infrastructure/bad",
	)
}
