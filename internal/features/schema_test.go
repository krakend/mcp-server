package features

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// testSchema mimics the shape of the KrakenD bundled schema: namespaces live under
// extra_config, definitions are referenced with $ref and some carry a local
// description that overrides the one of the referenced definition.
const testSchema = `{
  "$schema": "https://json-schema.org/draft/2019-09/schema",
  "type": "object",
  "properties": {
    "extra_config": {"$ref": "#/$defs/service_extra_config"},
    "endpoints": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "extra_config": {"$ref": "#/$defs/endpoint_extra_config"}
        }
      }
    }
  },
  "$defs": {
    "service_extra_config": {
      "type": "object",
      "properties": {
        "telemetry/logging": {"$ref": "#/$defs/logging"},
        "qos/ratelimit/service": {"description": "Enterprise only. Service rate limit"}
      }
    },
    "endpoint_extra_config": {
      "type": "object",
      "properties": {
        "telemetry/opentelemetry": {
          "description": "Enables metrics and traces",
          "$ref": "#/$defs/otel_endpoint"
        },
        "proxy": {
          "type": "object",
          "properties": {
            "max_payload": {"description": "Enterprise only. Limits the payload"},
            "sequential": {"description": "Sequential proxy"}
          }
        }
      },
      "patternProperties": {
        "^modifier/lua-": {"$ref": "#/$defs/lua"}
      }
    },
    "logging": {
      "description": "Logging",
      "type": "object",
      "properties": {
        "level": {"type": "string"},
        "access_log_format": {"description": "Enterprise only. Access log format"}
      }
    },
    "otel_endpoint": {
      "description": "Enterprise only. Overrides metrics and traces",
      "type": "object",
      "allOf": [{
        "properties": {
          "exporters_override": {"description": "Enterprise only. Override exporters"},
          "disable_metrics": {"type": "boolean"}
        }
      }]
    },
    "lua": {
      "description": "Lua scripting",
      "type": "object",
      "properties": {
        "pre": {"type": "string"},
        "sources": {"description": "Enterprise only. Fake marker", "type": "array"}
      }
    }
  }
}`

func compileTestSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(testSchema))
	if err != nil {
		t.Fatalf("invalid test schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("test.json", doc); err != nil {
		t.Fatalf("add resource: %v", err)
	}
	s, err := c.Compile("test.json")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return s
}

func parseTestConfig(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("invalid test config: %v", err)
	}
	return v
}

func TestFindEnterpriseUsages_CEConfig(t *testing.T) {
	config := parseTestConfig(t, `{
		"extra_config": {"telemetry/logging": {"level": "INFO"}},
		"endpoints": [{
			"extra_config": {
				"telemetry/opentelemetry": {"disable_metrics": true},
				"modifier/lua-proxy": {"pre": "print('x')"},
				"proxy": {"sequential": true}
			}
		}]
	}`)

	usages := FindEnterpriseUsages(compileTestSchema(t), config)
	if len(usages) != 0 {
		t.Errorf("expected no EE usages, got %v", usages)
	}
}

func TestFindEnterpriseUsages_EEConfig(t *testing.T) {
	config := parseTestConfig(t, `{
		"extra_config": {
			"telemetry/logging": {"level": "INFO", "access_log_format": "json"},
			"qos/ratelimit/service": {}
		},
		"endpoints": [
			{},
			{
				"extra_config": {
					"telemetry/opentelemetry": {"exporters_override": {}},
					"modifier/lua-proxy": {"sources": ["a.lua"]},
					"proxy": {"max_payload": 100}
				}
			}
		]
	}`)

	got := FindEnterpriseUsages(compileTestSchema(t), config)
	sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })

	want := []EnterpriseUsage{
		{Namespace: "modifier/lua-proxy", Path: "$.endpoints[1].extra_config.modifier/lua-proxy.sources"},
		{Namespace: "proxy", Path: "$.endpoints[1].extra_config.proxy.max_payload"},
		{Namespace: "telemetry/opentelemetry", Path: "$.endpoints[1].extra_config.telemetry/opentelemetry.exporters_override"},
		{Namespace: "qos/ratelimit/service", Path: "$.extra_config.qos/ratelimit/service"},
		{Namespace: "telemetry/logging", Path: "$.extra_config.telemetry/logging.access_log_format"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected usages:\n got: %v\nwant: %v", got, want)
	}
}

func TestExtraConfigNamespaces(t *testing.T) {
	config := parseTestConfig(t, `{
		"extra_config": {"telemetry/logging": {}, "router": {}},
		"endpoints": [{
			"extra_config": {"proxy": {}, "telemetry/logging": {}},
			"backend": [{"extra_config": {"modifier/martian": {"fields": {"a/b": 1}}}}]
		}]
	}`)

	got := ExtraConfigNamespaces(config)
	sort.Strings(got)

	want := []string{"modifier/martian", "proxy", "router", "telemetry/logging"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
