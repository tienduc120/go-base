package aggregateshape_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tienduc120/go-base/tools/analyzers/aggregateshape"
)

func TestAggregateShape(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), aggregateshape.New(),
		"example.com/svc/internal/domain/good",
		"example.com/svc/internal/domain/bad",
		"example.com/svc/internal/application/misuse",
		"example.com/svc/internal/infrastructure/ok",
	)
}
