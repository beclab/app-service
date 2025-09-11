package utils

import (
	"fmt"
	"k8s.io/apimachinery/pkg/util/validation"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// EnvNameToResourceName validates and converts an env name to a k8s resource name.
// - must start with a letter
// - allowed chars: letters, digits, underscore
// - output: lowercase, underscores converted to hyphens
func EnvNameToResourceName(envName string) (string, error) {
	if !envNamePattern.MatchString(envName) {
		return "", fmt.Errorf("invalid env name: must start with a letter and contain only letters, digits, and underscores")
	}

	result := strings.ToLower(envName)
	result = strings.ReplaceAll(result, "_", "-")
	return result, nil
}

func CheckEnvValueByType(value, valueType string) error {
	if value == "" {
		return nil
	}
	switch valueType {
	case "", "string", "password":
		return nil
	case "int":
		_, err := strconv.Atoi(value)
		return err
	case "bool":
		_, err := strconv.ParseBool(value)
		return err
	case "url":
		_, err := url.ParseRequestURI(value)
		return err
	case "ip":
		ip := net.ParseIP(value)
		if ip == nil {
			return fmt.Errorf("invalid ip '%s'", value)
		}
		return nil
	case "domain":
		errs := validation.IsDNS1123Subdomain(value)
		if len(errs) > 0 {
			return fmt.Errorf("invalid domain '%s'", value)
		}
		return nil
	case "email":
		_, err := mail.ParseAddress(value)
		if err != nil {
			return fmt.Errorf("invalid email '%s'", value)
		}
	}
	return nil
}
