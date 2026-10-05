package metadata

import (
	"reflect"
	"regexp"
)

var storedCustomVersion = regexp.MustCompile(`^custom-[0-9a-f]{64}$`)

// DecodeStored validates a document already accepted by the write boundary.
// It uses the saved custom definitions, never today's editable settings. Built-in
// standard-v1 definitions are frozen and cannot be overridden by a snapshot.
// Since snapshots contain only used definitions, they cannot independently prove
// the full registry hash; new writes must still use Validate with the repository's
// immutable registry. This decoder is not a client-input validation shortcut.
func DecodeStored(data []byte) (Document, error) {
	if len(data) > MaxDocumentBytes {
		return Document{}, invalid("document", "too_large")
	}
	var doc Document
	if err := DecodeJSON(data, &doc); err != nil {
		return Document{}, err
	}
	if doc.SchemaVersion != SchemaVersion || (doc.DefinitionsVersion != StandardDefinitionsVersion && !storedCustomVersion.MatchString(doc.DefinitionsVersion)) {
		return Document{}, ErrUnsupportedVersion
	}
	registry := StandardRegistry()
	standard := registry.Definitions
	registry.DefinitionsVersion = doc.DefinitionsVersion
	registry.Definitions = make(map[string]FieldDefinition, len(doc.DefinitionSnapshot))
	customCount := 0
	for key, def := range doc.DefinitionSnapshot {
		if expected, ok := standard[key]; ok {
			if !reflect.DeepEqual(expected, def) {
				return Document{}, invalid(key, "definition_mismatch")
			}
		} else {
			customCount++
			if doc.DefinitionsVersion == StandardDefinitionsVersion || !customKey.MatchString(key) || def.Key != key || customCount > MaxCustomFields {
				return Document{}, invalid(key, "unregistered_field")
			}
			if err := validateCustomDefinition(def); err != nil {
				return Document{}, err
			}
		}
		registry.Definitions[key] = def
	}
	return Decode(data, registry)
}
