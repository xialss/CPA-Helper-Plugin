package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// decodeObject rejects missing, null, duplicate and unknown fields before zero
// values can accidentally turn an incomplete permission document into a policy.
func decodeObject(raw []byte, out any, required ...string) error {
	// These flat policy structs all have explicit tags; do not inherit encoding/json's
	// case-insensitive aliases at the trust boundary.
	fields := map[string]bool{}
	typ := reflect.TypeOf(out).Elem()
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("policy value must be an object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return fmt.Errorf("invalid policy object")
		}
		name, ok := token.(string)
		if !ok || !fields[name] {
			return fmt.Errorf("unknown policy field")
		}
		if seen[name] {
			return fmt.Errorf("duplicate policy field")
		}
		seen[name] = true
		var value json.RawMessage
		if err = d.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("invalid or null policy field")
		}
	}
	if _, err = d.Token(); err != nil {
		return fmt.Errorf("invalid policy object")
	}
	if _, err = d.Token(); err != io.EOF {
		return fmt.Errorf("trailing policy data")
	}
	for _, name := range required {
		if !seen[name] {
			return fmt.Errorf("required policy field missing: %s", name)
		}
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return fmt.Errorf("invalid policy field or type")
	}
	return nil
}

// UnmarshalJSON enforces the required snapshot envelope at all input boundaries.
func (s *Snapshot) UnmarshalJSON(raw []byte) error {
	type plain Snapshot
	var value plain
	if err := decodeObject(raw, &value, "contract_version", "plugin_version", "policy_revision", "generated_at", "groups", "keys"); err != nil {
		return err
	}
	*s = Snapshot(value)
	return nil
}

// UnmarshalJSON requires explicit key enablement, bindings, limit and direct rule.
func (k *Key) UnmarshalJSON(raw []byte) error {
	type plain Key
	var value plain
	if err := decodeObject(raw, &value, "id", "enabled", "group_ids", "max_concurrency", "rule"); err != nil {
		return err
	}
	*k = Key(value)
	return nil
}

// UnmarshalJSON prevents an omitted rule from becoming an unrestricted group.
func (g *Group) UnmarshalJSON(raw []byte) error {
	type plain Group
	var value plain
	if err := decodeObject(raw, &value, "id", "name", "rule"); err != nil {
		return err
	}
	*g = Group(value)
	return nil
}

// UnmarshalJSON accepts an explicit empty rule, but not null or unknown fields.
func (r *Rule) UnmarshalJSON(raw []byte) error {
	type plain Rule
	var value plain
	if err := decodeObject(raw, &value); err != nil {
		return err
	}
	*r = Rule(value)
	return nil
}

// UnmarshalJSON requires both dimensions of a credential category.
func (s *Selector) UnmarshalJSON(raw []byte) error {
	type plain Selector
	var value plain
	if err := decodeObject(raw, &value, "source", "provider"); err != nil {
		return err
	}
	*s = Selector(value)
	return nil
}
