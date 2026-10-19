# OpenAPI test fixtures

`oas-3.1-schema.json` is the OpenAPI 3.1 document schema, retrieved on 2026-10-17 from <https://spec.openapis.org/oas/3.1/schema/2022-10-07>. `TestOpenAPIDocumentValidatesAgainstOAS31` validates the emitted document against it. Refresh it from the same URL; a newer dated schema replaces both the file and this date.

`types.go` holds the Go types the OpenAPI tests register.
