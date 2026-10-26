package features

import (
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// enterpriseMarker is the prefix the KrakenD JSON schema uses in the description
// of every namespace or attribute that is only available in the Enterprise Edition
const enterpriseMarker = "Enterprise only"

// EnterpriseUsage is a config location whose schema is flagged as Enterprise only
type EnterpriseUsage struct {
	Namespace string `json:"namespace"` // extra_config key enclosing the usage
	Path      string `json:"path"`      // JSON path of the flagged key, e.g. $.extra_config.telemetry/logging.access_log_format
}

// FindEnterpriseUsages walks the config alongside the compiled KrakenD schema and
// returns every key whose schema description starts with "Enterprise only".
// Descendants of a flagged key are not reported.
func FindEnterpriseUsages(schema *jsonschema.Schema, config any) []EnterpriseUsage {
	var usages []EnterpriseUsage
	walkSchema(config, schema, "$", "", false, &usages)
	return usages
}

func walkSchema(value any, schema *jsonschema.Schema, path, namespace string, inExtraConfig bool, usages *[]EnterpriseUsage) {
	if schema == nil {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			childSchema := propertySchema(schema, key)
			if childSchema == nil {
				continue
			}
			childNamespace := namespace
			if inExtraConfig {
				childNamespace = key
			}
			childPath := path + "." + key
			if strings.HasPrefix(effectiveDescription(childSchema), enterpriseMarker) {
				*usages = append(*usages, EnterpriseUsage{Namespace: childNamespace, Path: childPath})
				continue
			}
			walkSchema(child, childSchema, childPath, childNamespace, key == "extra_config", usages)
		}
	case []any:
		itemSchema := arrayItemSchema(schema)
		for i, child := range v {
			walkSchema(child, itemSchema, fmt.Sprintf("%s[%d]", path, i), namespace, false, usages)
		}
	}
}

// effectiveDescription returns the node's own description, falling back to the one
// of the schema it references. A local description must win: the schema overrides
// the description of a shared definition where its meaning differs in that context.
func effectiveDescription(s *jsonschema.Schema) string {
	for ; s != nil; s = s.Ref {
		if s.Description != "" {
			return s.Description
		}
	}
	return ""
}

// composedSchemas returns the schema plus all the schemas it is composed of
func composedSchemas(s *jsonschema.Schema, out []*jsonschema.Schema) []*jsonschema.Schema {
	if s == nil {
		return out
	}
	out = append(out, s)
	out = composedSchemas(s.Ref, out)
	for _, group := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf} {
		for _, sub := range group {
			out = composedSchemas(sub, out)
		}
	}
	return out
}

func propertySchema(s *jsonschema.Schema, key string) *jsonschema.Schema {
	candidates := composedSchemas(s, nil)
	for _, c := range candidates {
		if p, ok := c.Properties[key]; ok {
			return p
		}
	}
	for _, c := range candidates {
		for re, p := range c.PatternProperties {
			if re.MatchString(key) {
				return p
			}
		}
		if p, ok := c.AdditionalProperties.(*jsonschema.Schema); ok {
			return p
		}
	}
	return nil
}

func arrayItemSchema(s *jsonschema.Schema) *jsonschema.Schema {
	for _, c := range composedSchemas(s, nil) {
		if c.Items2020 != nil {
			return c.Items2020
		}
		if item, ok := c.Items.(*jsonschema.Schema); ok {
			return item
		}
	}
	return nil
}

// ExtraConfigNamespaces returns the keys of every extra_config object in the config
func ExtraConfigNamespaces(config any) []string {
	seen := make(map[string]struct{})
	collectExtraConfigKeys(config, seen)

	namespaces := make([]string, 0, len(seen))
	for ns := range seen {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

func collectExtraConfigKeys(data any, seen map[string]struct{}) {
	switch v := data.(type) {
	case map[string]any:
		for key, value := range v {
			if extra, ok := value.(map[string]any); ok && key == "extra_config" {
				for ns, nsValue := range extra {
					seen[ns] = struct{}{}
					collectExtraConfigKeys(nsValue, seen)
				}
				continue
			}
			collectExtraConfigKeys(value, seen)
		}
	case []any:
		for _, item := range v {
			collectExtraConfigKeys(item, seen)
		}
	}
}
