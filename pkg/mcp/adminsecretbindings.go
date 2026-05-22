package mcp

// AdminSecretBindingsAnnotation records secretBinding fields chosen on the deployed server,
// so Obot can distinguish admin config from git-ops catalog secretBinding changes.
const AdminSecretBindingsAnnotation = "obot.obot.ai/admin-secret-bindings"

type SecretBindingRefs struct {
	Env     map[string]struct{}
	Headers map[string]struct{}
}

type SecretBindingRefsAnnotation struct {
	Env     []string `json:"env,omitempty"`
	Headers []string `json:"headers,omitempty"`
}
