package docs
// Package docs embeds the OpenAPI specification so it can be served at









var OpenAPISpec []byte//go:embed openapi.yaml//// OpenAPISpec is the raw OpenAPI 3.0 YAML for the go-orca API.import _ "embed"package docs// runtime by the API server without requiring the file to be present on disk.