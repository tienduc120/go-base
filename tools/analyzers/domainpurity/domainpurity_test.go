package domainpurity_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tienduc120/go-base/tools/analyzers/domainpurity"
)

func TestDomainPurity(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), domainpurity.New(),
		"example.com/svc/internal/domain/bad",
		"example.com/svc/internal/domain/good",
		"example.com/svc/internal/application/other",
	)
}
