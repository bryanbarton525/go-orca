// Package docs embeds the OpenAPI specification so it can be served at
// runtime by the API server without requiring the file to be present on disk.
package docs

import _ "embed"

//go:embed openapi.yaml
var OpenAPISpec []byte
