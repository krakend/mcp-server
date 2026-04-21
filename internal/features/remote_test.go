package features

import (
	"errors"
	"testing"
)

const minimalYAML = `
sections:
  - name: "Traffic Management"
    description: "Traffic control features"
    features:
      - name: "Rate Limiting"
        description: "Limit request rates"
        url: "/docs/rate-limiting"
        ee: false
        namespaces:
          - "qos/ratelimit/router"
      - name: "Redis Rate Limiting"
        description: "Distributed rate limiting"
        url: "/docs/redis-rate-limiting"
        ee: true
        namespaces:
          - "qos/ratelimit/redis"
`

func TestParseRemoteYAML_BasicParsing(t *testing.T) {
	catalog, matrix, err := parseRemoteYAML([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalog.Features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(catalog.Features))
	}

	f := catalog.Features[0]
	if f.Name != "Rate Limiting" {
		t.Errorf("expected name 'Rate Limiting', got %q", f.Name)
	}
	if f.Category != "Traffic Management" {
		t.Errorf("expected category 'Traffic Management', got %q", f.Category)
	}
	if f.Namespace != "qos/ratelimit/router" {
		t.Errorf("expected namespace 'qos/ratelimit/router', got %q", f.Namespace)
	}
	if f.DocsURL != "https://www.krakend.io/docs/rate-limiting" {
		t.Errorf("expected normalized docs URL, got %q", f.DocsURL)
	}
	if f.ID != "qos/ratelimit/router" {
		t.Errorf("expected ID to be namespace, got %q", f.ID)
	}
	_ = matrix
}

func TestParseRemoteYAML_EEFlag(t *testing.T) {
	catalog, matrix, err := parseRemoteYAML([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ce := catalog.Features[0]
	if ce.Edition != "ce" {
		t.Errorf("expected edition 'ce', got %q", ce.Edition)
	}

	ee := catalog.Features[1]
	if ee.Edition != "ee" {
		t.Errorf("expected edition 'ee', got %q", ee.Edition)
	}

	if len(matrix.CEFeatures) != 1 || matrix.CEFeatures[0] != "qos/ratelimit/router" {
		t.Errorf("unexpected CE features: %v", matrix.CEFeatures)
	}
	if len(matrix.EEOnlyFeatures) != 1 || matrix.EEOnlyFeatures[0] != "qos/ratelimit/redis" {
		t.Errorf("unexpected EE features: %v", matrix.EEOnlyFeatures)
	}
}

func TestParseRemoteYAML_MultipleNamespaces(t *testing.T) {
	data := `
sections:
  - name: "Auth"
    features:
      - name: "Multi-NS Feature"
        ee: true
        namespaces:
          - "auth/ns1"
          - "auth/ns2"
`
	catalog, matrix, err := parseRemoteYAML([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First namespace is used for feature.Namespace
	if catalog.Features[0].Namespace != "auth/ns1" {
		t.Errorf("expected first namespace, got %q", catalog.Features[0].Namespace)
	}

	// Both namespaces go into EEOnlyFeatures
	if len(matrix.EEOnlyFeatures) != 2 {
		t.Errorf("expected 2 EE namespaces, got %d: %v", len(matrix.EEOnlyFeatures), matrix.EEOnlyFeatures)
	}
}

func TestParseRemoteYAML_NoNamespace(t *testing.T) {
	data := `
sections:
  - name: "General"
    features:
      - name: "No Namespace Feature"
        description: "A feature without namespaces"
        ee: false
`
	catalog, matrix, err := parseRemoteYAML([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Features without namespaces are skipped
	if len(catalog.Features) != 0 {
		t.Fatalf("expected 0 features, got %d", len(catalog.Features))
	}
	if len(matrix.CEFeatures) != 0 {
		t.Errorf("expected 0 CE features, got %d", len(matrix.CEFeatures))
	}
}

func TestParseRemoteYAML_RelativeURL(t *testing.T) {
	data := `
sections:
  - name: "Docs"
    features:
      - name: "Feature"
        url: "/docs/feature"
        ee: false
        namespaces:
          - "some/namespace"
`
	catalog, _, err := parseRemoteYAML([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if catalog.Features[0].DocsURL != "https://www.krakend.io/docs/feature" {
		t.Errorf("expected normalized URL, got %q", catalog.Features[0].DocsURL)
	}
}

func TestFetchRemoteFeatureMatrix_FetchError(t *testing.T) {
	orig := HTTPFetcher
	HTTPFetcher = func(_ string) ([]byte, error) { return nil, errors.New("connection refused") }
	t.Cleanup(func() { HTTPFetcher = orig })

	_, _, err := FetchRemoteFeatureMatrix("https://example.com/matrix.yaml")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchRemoteFeatureMatrix_ParseError(t *testing.T) {
	orig := HTTPFetcher
	HTTPFetcher = func(_ string) ([]byte, error) { return []byte(":\tinvalid: yaml: ]["), nil }
	t.Cleanup(func() { HTTPFetcher = orig })

	_, _, err := FetchRemoteFeatureMatrix("https://example.com/matrix.yaml")
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}
