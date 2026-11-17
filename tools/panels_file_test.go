package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/krakend/mcp-server/internal/observability"
)

func TestProjectJSONPath(t *testing.T) {
	wd, _ := os.Getwd()
	if p, err := projectJSONPath("out/panels.json"); err != nil || p != filepath.Join(wd, "out/panels.json") {
		t.Errorf("relative path inside the project: %q %v", p, err)
	}
	for _, bad := range []string{"../panels.json", "/etc/panels.json", "panels.txt", "a/../../x.json"} {
		if _, err := projectJSONPath(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestWritePanelsFile(t *testing.T) {
	dir := t.TempDir()
	resp := &observability.RecipeResponse{Variables: []string{"job"}, Recipes: []observability.RenderedRecipe{
		{ID: "traffic.rps_total", GrafanaPanel: map[string]any{"title": "Requests / s", "type": "stat"}},
	}}
	path := filepath.Join(dir, "p.json")
	if err := writePanelsFile(path, resp); err != nil {
		t.Fatal(err)
	}
	if resp.Recipes[0].GrafanaPanel != nil || resp.Recipes[0].Title != "Requests / s" || resp.PanelsFile != path {
		t.Errorf("response must drop the panel, keep the title and say where the file is: %+v", resp)
	}
	var f struct {
		Order  []string                  `json:"order"`
		Panels map[string]map[string]any `json:"panels"`
	}
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &f); err != nil || f.Order[0] != "traffic.rps_total" || f.Panels["traffic.rps_total"]["type"] != "stat" {
		t.Errorf("bad panels file: %s %v", b, err)
	}
}
