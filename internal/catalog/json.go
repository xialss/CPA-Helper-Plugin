package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Every catalog field is required. Reject ambiguous names and values before
// encoding/json can merge duplicate fields or accept case-insensitive aliases.
func decodeObject(raw []byte, out any, fields ...string) error {
	required := make(map[string]bool, len(fields))
	for _, name := range fields {
		required[name] = true
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("catalog value must be an object")
	}
	seen := make(map[string]bool, len(fields))
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return errors.New("invalid catalog object")
		}
		name, ok := token.(string)
		if !ok || !required[name] {
			return errors.New("unknown catalog field")
		}
		if seen[name] {
			return errors.New("duplicate catalog field")
		}
		seen[name] = true
		var value json.RawMessage
		if err = d.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("invalid or null catalog field")
		}
	}
	if _, err = d.Token(); err != nil {
		return errors.New("invalid catalog object")
	}
	if _, err = d.Token(); err != io.EOF {
		return errors.New("trailing catalog data")
	}
	for _, name := range fields {
		if !seen[name] {
			return fmt.Errorf("required catalog field missing: %s", name)
		}
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return errors.New("invalid catalog field or type")
	}
	return nil
}

// UnmarshalJSON enforces the complete envelope at management and storage boundaries.
func (s *Snapshot) UnmarshalJSON(raw []byte) error {
	type plain Snapshot
	var value plain
	if err := decodeObject(raw, &value, "contract_version", "plugin_version", "cpa_version", "catalog_revision", "generated_at", "credentials"); err != nil {
		return err
	}
	*s = Snapshot(value)
	return nil
}

// UnmarshalJSON requires explicit routing attributes without duplicate or alias fields.
func (c *Credential) UnmarshalJSON(raw []byte) error {
	type plain Credential
	var value plain
	if err := decodeObject(raw, &value, "ref", "source", "provider", "enabled", "models"); err != nil {
		return err
	}
	*c = Credential(value)
	return nil
}
