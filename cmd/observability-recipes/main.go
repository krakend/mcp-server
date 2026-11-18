// Command observability-recipes regenerates tools/data/observability/recipes.json from the
// hand-edited observability/promql-recipes.md and observability/blueprints.md.
//
// Run it from the repository root after editing either file:
//
//	go run ./cmd/observability-recipes
//
// It fails, listing every problem, on unknown metrics, duplicate legends, "/ s" titles on
// counts, missing $backend_host or 404 filters, recipe ids in panel notes, and blueprints that
// name unknown recipes.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/krakend/mcp-server/internal/observability/recipesgen"
)

func main() {
	md := flag.String("md", "observability/promql-recipes.md", "recipes source")
	bp := flag.String("blueprints", "observability/blueprints.md", "topic dashboard layouts source")
	cat := flag.String("catalog", "tools/data/observability/catalog.json", "metrics catalog")
	out := flag.String("out", "tools/data/observability/recipes.json", "output file")
	flag.Parse()

	read := func(path string) []byte {
		b, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v (run from the repository root)\n", err)
			os.Exit(1)
		}
		return b
	}
	res, err := recipesgen.Build(string(read(*md)), string(read(*bp)), read(*cat))
	if err != nil {
		var problems *recipesgen.Error
		if errors.As(err, &problems) {
			fmt.Fprintln(os.Stderr, problems.Error())
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	if err := os.WriteFile(*out, res.JSON, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d recipes -> %s  (sections: %d, may_be_empty: %d)\n", res.Recipes, *out, res.Sections, res.MayBeEmpty)
}
