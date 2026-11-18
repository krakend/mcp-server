# Observability data sources

Sources of the data embedded in `tools/data/observability/`, used by `get_metrics_catalog`,
`analyze_config_observability`, `get_dashboard_recipes` and `lint_dashboard`. Edit the sources
here and regenerate; never edit the generated JSON by hand.

| File | What it is |
|---|---|
| `promql-recipes.md` | Hand-edited PromQL recipes (tables per section: id, title, panel, query, unit). |
| `blueprints.md` | Hand-edited layouts of topic dashboards (overview, runtime, backends, agents). |
| `../tools/data/observability/catalog.json` | Generated metrics catalog: every KrakenD metric, its meaning, labels and the config that enables it. |
| `../tools/data/observability/recipes.json` | Generated from the two files above and the catalog. |

## Recipes or blueprints changed

```bash
go run ./cmd/observability-recipes
go test ./internal/observability/... ./tools
```

The generator fails, listing every problem, on unknown metrics, duplicate legends, titles that
don't match the query (per second vs count), missing `$backend_host` or 404 filters, recipe ids in
panel notes (panel descriptions are read by dashboard users: name the panel instead), and
blueprints that name unknown recipes. `TestCommittedRecipesMatchSources` fails when
`recipes.json` wasn't regenerated after an edit.

## New KrakenD version (new or renamed metrics)

`catalog.json` comes from running a KrakenD EE build with every component enabled and traffic
through every feature (instruments only appear after their first use), in both semantic
conventions. That run needs an Enterprise build, so it is done by the KrakenD team outside this
repository; the result is committed here as `catalog.json`. Then regenerate the recipes as above:
recipes that use a metric the new catalog no longer has make the generator fail.
