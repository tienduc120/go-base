// Command agentlint bundles the repository's own analyzers as a go vet tool: go vet -vettool=agentlint ./...
package main

import (
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/tienduc120/go-base/tools/analyzers/barego"
	"github.com/tienduc120/go-base/tools/analyzers/domainpurity"
)

func main() {
	unitchecker.Main(barego.New(), domainpurity.New())
}
