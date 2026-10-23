// Package schemas embeds the JSON contracts shared with agents.
package schemas

import _ "embed"

// InterpretationV1ID is the $id of the interpretation submission schema.
const InterpretationV1ID = "https://thesmothetai.local/schemas/interpretation.v1.json"

//go:embed interpretation.v1.json
var InterpretationV1 []byte
