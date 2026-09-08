// Package docs embeds the hub's OpenAPI specification so it can be served
// directly from the running binary without depending on an external file
// on disk (which would be missing from a deployed binary that isn't
// shipped alongside the repository).
package docs

import _ "embed"

// OpenAPISpec is the raw OpenAPI 3.1 document served at
// GET /api/custom/openapi.yaml. It documents every route registered by
// internal/hub/api.RegisterRoutes and RegisterAlertRoutes, plus the
// unauthenticated /healthz route; see
// internal/hub/api/openapi_test.go for the test that keeps this document
// in sync with the actual router registration.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
