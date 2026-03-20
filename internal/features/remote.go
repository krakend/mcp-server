package features

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// HTTPFetcher is the function used to fetch remote URLs. It can be replaced in tests.
var HTTPFetcher func(url string) ([]byte, error) = defaultHTTPFetch

func defaultHTTPFetch(url string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	buf := make([]byte, 0, 1024*1024)
	tmp := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

// remoteMatrix is the top-level YAML structure.
type remoteMatrix struct {
	Sections []remoteSection `yaml:"sections"`
}

type remoteSection struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Features    []remoteFeature `yaml:"features"`
}

type remoteFeature struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	URL         string   `yaml:"url"`
	EE          bool     `yaml:"ee"`
	Namespaces  []string `yaml:"namespaces"`
}

// FetchRemoteFeatureMatrix downloads and parses the remote YAML feature matrix.
func FetchRemoteFeatureMatrix(url string) (*FeatureCatalog, *EditionMatrix, error) {
	data, err := HTTPFetcher(url)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	return ParseFeatureMatrix(data)
}

// ParseFeatureMatrix parses raw YAML feature matrix data into catalog and edition matrix.
func ParseFeatureMatrix(data []byte) (*FeatureCatalog, *EditionMatrix, error) {
	return parseRemoteYAML(data)
}

func parseRemoteYAML(data []byte) (*FeatureCatalog, *EditionMatrix, error) {
	var rm remoteMatrix
	if err := yaml.Unmarshal(data, &rm); err != nil {
		return nil, nil, fmt.Errorf("parse yaml: %w", err)
	}

	catalog := &FeatureCatalog{}
	matrix := &EditionMatrix{
		CEFeatures:     []string{},
		EEOnlyFeatures: []string{},
		FeatureDetails: map[string]map[string]interface{}{},
	}

	for _, section := range rm.Sections {
		for _, rf := range section.Features {
			if len(rf.Namespaces) == 0 {
				continue
			}

			edition := "ce"
			if rf.EE {
				edition = "ee"
			}

			f := Feature{
				ID:          rf.Namespaces[0],
				Name:        rf.Name,
				Namespace:   rf.Namespaces[0],
				Edition:     edition,
				Category:    section.Name,
				Description: rf.Description,
				DocsURL:     normalizeURL(rf.URL),
			}
			catalog.Features = append(catalog.Features, f)

			for _, ns := range rf.Namespaces {
				if rf.EE {
					matrix.EEOnlyFeatures = append(matrix.EEOnlyFeatures, ns)
				} else {
					matrix.CEFeatures = append(matrix.CEFeatures, ns)
				}
			}
		}
	}

	return catalog, matrix, nil
}

// normalizeURL prepends the KrakenD base URL for relative paths.
func normalizeURL(u string) string {
	if strings.HasPrefix(u, "/") {
		return "https://www.krakend.io" + u
	}
	return u
}

