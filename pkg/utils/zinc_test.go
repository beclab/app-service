package utils

import (
	"strings"
	"testing"
)

func TestCheckZincSearchMappings(t *testing.T) {
	testCases := []struct {
		Properties string
		Expected   string
	}{
		{
			Properties: `  {
		   "properties": {
		       "title": {
		           "type": "text",
		           "index": true,
		           "store": true,
		           "highlightable": true
		       },
		       "content": {
		           "type": "text",
		           "index": true,
		           "store": true,
		           "highlightable": true
		       },
		       "status": {
		           "type": "keyword",
		           "index": true,
		           "sortable": true,
		           "aggregatable": true
		       },
		       "publish_date": {
		           "type": "date",
		           "format": "2006-01-02T15:04:05Z07:00",
		           "index": true,
		           "sortable": true,
		           "aggregatable": true
		       }
		   }
		}`,
			Expected: "",
		},
		{
			Properties: ``,
			Expected:   "unexpected end of JSON input",
		},
		{
			Properties: `properties`,
			Expected:   "invalid character 'p' looking for beginning of value",
		},
	}
	for _, test := range testCases {
		r := strings.NewReader(test.Properties)

		err := CheckZincSearchMappings(r)
		if err != nil {
			if err.Error() != test.Expected {
				t.Error("zinc mappings file format error", err)
			}
		} else {
			if test.Expected != "" {
				t.Error("zinc mappings file format error", err)
			}
		}

	}
}
