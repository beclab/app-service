package sidecar

import (
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestGenEnvoySetCookieScript(t *testing.T) {
	s, err := genEnvoySetCookieScript([]string{"gitlab.pp.snowinning.com", "gitlab.local.pp.snowinning.com"})
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.ReplaceAll(envoySetCookie, `local app_domains = { {{- range $i, $appDomain := .AppDomains }} {{- if gt $i 0 -}},{{ end }} "{{ $appDomain }}" {{- end }} }`, `local app_domains = { "gitlab.pp.snowinning.com", "gitlab.local.pp.snowinning.com" }`)
	assert.Equal(t, expected, string(s))
}
