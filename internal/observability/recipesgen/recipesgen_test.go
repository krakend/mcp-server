package recipesgen

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const root = "../../../"

func sources(t *testing.T) (string, string, []byte) {
	t.Helper()
	read := func(p string) []byte {
		b, err := os.ReadFile(root + p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return string(read("observability/promql-recipes.md")), string(read("observability/blueprints.md")),
		read("tools/data/observability/catalog.json")
}

// The embedded recipes.json must be what its sources generate: edit the markdown, then run
// `go run ./cmd/observability-recipes`.
func TestCommittedRecipesMatchSources(t *testing.T) {
	md, bp, cat := sources(t)
	res, err := Build(md, bp, cat)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(root + "tools/data/observability/recipes.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res.JSON, committed) {
		t.Fatal("tools/data/observability/recipes.json is out of date: run `go run ./cmd/observability-recipes`")
	}
}

// Each check must still catch its mistake.
func TestBuildRejectsInconsistentRecipes(t *testing.T) {
	md, bp, cat := sources(t)
	cases := []struct {
		name, md, bp, want string
	}{
		{"unknown metric", strings.Replace(md, "http_server_duration_count{SEL}", "http_server_nope_count{SEL}", 1), bp, "metric http_server_nope_count not in catalog"},
		{"per-second title on a count", md +
			"\n## Traffic\n\n| id | title | panel | query | unit |\n|---|---|---|---|---|\n| `traffic.x` | X / s | stat | `sum(increase(http_server_duration_count{SEL, http_route!=\"404 Not Found\"}[$__range]))` | short |\n",
			bp, "says '/ s' but the query isn't a rate"},
		{"recipe id in notes", strings.Replace(md, `unknown paths are counted in the "404 Not Found / s" panel`,
			"unknown paths are in traffic.not_found_rate", 1), bp, "notes mention recipe id traffic.not_found_rate"},
		{"unknown blueprint recipe", md, bp + "\n## extra\n\n- Stats: `traffic.nope`\n", "lists unknown recipe traffic.nope"},
	}
	for _, c := range cases {
		_, err := Build(c.md, c.bp, cat)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error containing %q, got %v", c.name, c.want, err)
		}
	}
}

func TestPythonCompatibleJSON(t *testing.T) {
	got := string(Marshal(Obj{{"a", Float(1)}, {"b", Float(0.001)}, {"c", Float(0.00001)}, {"d", 1}, {"e", "× ≤ \"q\""}, {"f", []any{}}, {"g", nil}}))
	want := "{\n  \"a\": 1.0,\n  \"b\": 0.001,\n  \"c\": 1e-05,\n  \"d\": 1,\n  \"e\": \"\\u00d7 \\u2264 \\\"q\\\"\",\n  \"f\": [],\n  \"g\": null\n}"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
