package openapi

import (
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/obot-platform/obot/apiclient/types"
)

const maxCredentialHeadersBytes = 96 * 1024

// SnapshotEnvironment constructs deployment settings from the saved snapshot,
// never Source. The OpenAPI loader reads the top-level servers; import-time
// operation validation and header suggestions are not repeated here.
func SnapshotEnvironment(config types.OpenAPIRuntimeConfig, headers []types.MCPConfig) ([]string, error) {
	if config.Schema == nil || len(config.Schema.Raw) == 0 || len(config.Schema.Raw) > MaxSchemaBytes {
		return nil, fmt.Errorf("a stored OpenAPI schema of at most 1 MiB is required")
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	document, err := loader.LoadFromData(config.Schema.Raw)
	if err != nil || document == nil {
		return nil, fmt.Errorf("cannot parse stored OpenAPI schema")
	}
	base, err := findBaseURL(config.BaseURL, document.Servers)
	if err != nil {
		return nil, err
	}
	return Environment(&Result{BaseURL: base}, headers)
}

// Environment validates credential definitions and returns the wrapper's
// environment variables. Credential values and prefixes stay in Obot's
// per-request header handling. Definitions can change after schema import.
func Environment(result *Result, headers []types.MCPConfig) ([]string, error) {
	if result == nil {
		return nil, fmt.Errorf("a validated schema import is required")
	}
	names := make([]string, 0, len(headers))
	seen := map[string]bool{}
	for _, header := range headers {
		if header.Usage != types.Header {
			return nil, fmt.Errorf("OpenAPI credentials must be header inputs")
		}
		if err := validateHeader(header.Key); err != nil {
			return nil, err
		}
		key := strings.ToLower(header.Key)
		if seen[key] {
			return nil, fmt.Errorf("duplicate credential header names")
		}
		seen[key] = true
		names = append(names, header.Key)
	}
	base, err := destination(result.BaseURL)
	if err != nil {
		return nil, err
	}
	if len(names) > 0 && !strings.HasPrefix(base, "https://") {
		return nil, fmt.Errorf("credential forwarding requires an HTTPS API destination")
	}
	credentialHeaders := strings.Join(names, ",")
	if len(credentialHeaders) > maxCredentialHeadersBytes {
		return nil, fmt.Errorf("credential headers exceed 96 KiB")
	}
	return []string{
		"OPENAPI_BASE_URL=" + base,
		"OPENAPI_CREDENTIAL_HEADERS=" + credentialHeaders,
	}, nil
}
