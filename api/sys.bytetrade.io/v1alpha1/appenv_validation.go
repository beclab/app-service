package v1alpha1

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ValidateValueAgainstOptions validates the given value against Options and/or RemoteOptions.
// Rules:
// - If both Options and RemoteOptions are set, value is valid if it is in either set.
// - If only Options is set, value must be in Options.
// - If only RemoteOptions is set, value must be in the fetched remote list.
// - If neither is set, any value is accepted.
func (e *AppEnvVar) ValidateValueAgainstOptions(value string) error {
	if value == "" {
		return nil
	}
	hasOptions := len(e.Options) > 0
	hasRemote := strings.TrimSpace(e.RemoteOptions) != ""

	if !hasOptions && !hasRemote {
		return nil
	}

	if hasOptions && hasRemote {
		if optionsContainValue(e.Options, value) {
			return nil
		}
		allowed, err := fetchRemoteOptions(e.RemoteOptions)
		if err != nil {
			return fmt.Errorf("invalid remoteOptions: %w", err)
		}
		if !optionsContainValue(allowed, value) {
			return fmt.Errorf("value not allowed by options or remoteOptions")
		}
		return nil
	}

	if hasOptions {
		if !optionsContainValue(e.Options, value) {
			return fmt.Errorf("value not in options")
		}
		return nil
	}

	allowed, err := fetchRemoteOptions(e.RemoteOptions)
	if err != nil {
		return fmt.Errorf("invalid remoteOptions: %w", err)
	}
	if !optionsContainValue(allowed, value) {
		return fmt.Errorf("value not in remoteOptions")
	}
	return nil
}

func optionsContainValue(options []EnvValueOptionItem, v string) bool {
	for _, item := range options {
		if item.Value == v {
			return true
		}
	}
	return false
}

// fetchRemoteOptions fetches allowed values from a remote URL.
// Response body must be a JSON array of EnvValueOptionItem: [{"title":"A","value":"a"}, ...]
func fetchRemoteOptions(endpoint string) ([]EnvValueOptionItem, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse url failed: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	resp, err := http.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}
	var items []EnvValueOptionItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("decode json failed: %w", err)
	}
	return items, nil
}
