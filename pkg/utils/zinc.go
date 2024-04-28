package utils

import (
	"encoding/json"
	"io"
	"io/ioutil"
)

// Mappings represents a zinc search mappings.
type Mappings struct {
	Properties map[string]Property `json:"properties,omitempty"`
}

// Property represents a key details.
type Property struct {
	Type           string `json:"type"` // text, keyword, date, numeric, boolean, geo_point
	Analyzer       string `json:"analyzer,omitempty"`
	SearchAnalyzer string `json:"search_analyzer,omitempty"`
	Format         string `json:"format,omitempty"`    // date format yyyy-MM-dd HH:mm:ss || yyyy-MM-dd || epoch_millis
	TimeZone       string `json:"time_zone,omitempty"` // date format time_zone
	Index          bool   `json:"index"`
	Store          bool   `json:"store"`
	Sortable       bool   `json:"sortable"`
	Aggregatable   bool   `json:"aggregatable"`
	Highlightable  bool   `json:"highlightable"`

	Fields map[string]Property `json:"fields,omitempty"`
}

// CheckZincSearchMappings returns nil if zinc index.json is valid.
func CheckZincSearchMappings(f io.Reader) error {
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	var m Mappings
	err = json.Unmarshal(data, &m)
	if err != nil {
		return err
	}
	return nil
}
