package boatstack

import (
	"encoding/json"
	"fmt"
	"strings"
)

// deliveryStateMigration upgrades a managed delivery-state document one schema
// version forward. `from` and `to` bound the step; `apply` transforms the decoded
// document. The registered set is empty today — schema version 1 is the only
// version that has ever been written — so migration is a behavior-preserving
// pass-through. It exists so a future deliveryStateSchemaVersion bump has a
// defined, tested upgrade path instead of silently failing old state closed.
type deliveryStateMigration struct {
	from  int
	to    int
	apply func(map[string]any) (map[string]any, error)
}

// deliveryStateMigrations is the ordered, one-step-at-a-time upgrade chain for the
// managed delivery state. It is intentionally empty at schema version 1; a schema
// bump adds one entry per version step, each covered by conformance.
var deliveryStateMigrations []deliveryStateMigration

// migrateDeliveryStateBytes upgrades raw managed-delivery-state JSON to the current
// deliveryStateSchemaVersion using the registered migration chain. It is the
// production wrapper over the pure migrateDeliveryStateBytesWith.
func migrateDeliveryStateBytes(raw []byte) (upgraded []byte, changed bool, err error) {
	return migrateDeliveryStateBytesWith(raw, deliveryStateMigrations, deliveryStateSchemaVersion)
}

// migrateDeliveryStateBytesWith is the pure migration engine, taking its migration
// chain and target version as parameters so it is testable independently of the
// production (empty) chain. It mirrors MigrateConfigBytes exactly:
//
//   - blank input is a no-op pass-through (nothing to migrate);
//   - a document already at the target version passes through byte-for-byte
//     unchanged (the only path that runs today at version 1);
//   - an older document is walked forward one registered step at a time and its
//     schema_version stamped to the target;
//   - a document written by a NEWER Boatstack fail-closes with an actionable
//     message rather than being silently treated as corrupt;
//   - a missing step in the chain fail-closes rather than guessing.
func migrateDeliveryStateBytesWith(raw []byte, migrations []deliveryStateMigration, target int) (upgraded []byte, changed bool, err error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return raw, false, nil
	}

	var partial map[string]any
	if err := json.Unmarshal(raw, &partial); err != nil {
		return nil, false, fmt.Errorf("failed to parse delivery state JSON: %w", err)
	}

	fromVer := 1 // a document with no schema_version predates versioning: treat as v1.
	if v, ok := partial["schema_version"]; ok {
		switch val := v.(type) {
		case float64:
			fromVer = int(val)
		case int:
			fromVer = val
		default:
			return nil, false, fmt.Errorf("delivery state schema_version must be an integer")
		}
	}

	if fromVer > target {
		return nil, false, fmt.Errorf("managed delivery state was written by a newer Boatstack (schema %d > %d); update Boatstack", fromVer, target)
	}
	if fromVer == target {
		return raw, false, nil
	}

	current := fromVer
	data := partial
	for current < target {
		var found *deliveryStateMigration
		for i := range migrations {
			if migrations[i].from == current {
				found = &migrations[i]
				break
			}
		}
		if found == nil {
			return nil, false, fmt.Errorf("no delivery-state migration found from schema %d toward %d", current, target)
		}
		if found.to <= current {
			return nil, false, fmt.Errorf("invalid delivery-state migration path from %d to %d", found.from, found.to)
		}
		data, err = found.apply(data)
		if err != nil {
			return nil, false, fmt.Errorf("failed to apply delivery-state migration from %d to %d: %w", found.from, found.to, err)
		}
		current = found.to
	}

	data["schema_version"] = target
	upgraded, err = MarshalJSON(data)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal migrated delivery state: %w", err)
	}
	return upgraded, true, nil
}
