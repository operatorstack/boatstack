package boatstack

import (
	"encoding/json"
	"fmt"
	"strings"
)

const CurrentConfigSchemaVersion = 1

var currentConfigSchemaVersionOverride = CurrentConfigSchemaVersion

type configMigration struct {
	from  int
	to    int
	apply func(map[string]any) (map[string]any, error)
}

var configMigrations []configMigration

func currentSchemaVersion() int {
	return currentConfigSchemaVersionOverride
}

// MigrateConfigBytes migrates raw JSON configuration bytes to the latest CurrentConfigSchemaVersion.
// It returns the upgraded JSON bytes, the original schema version, the final schema version,
// a boolean indicating whether the content actually changed, and any error encountered.
func MigrateConfigBytes(raw []byte) (upgraded []byte, from, to int, changed bool, err error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return raw, 0, 0, false, nil
	}

	var partial map[string]any
	if err := json.Unmarshal(raw, &partial); err != nil {
		return nil, 0, 0, false, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	targetVer := currentSchemaVersion()

	var fromVer int
	if v, exists := partial["schema_version"]; exists {
		switch val := v.(type) {
		case float64:
			fromVer = int(val)
		case int:
			fromVer = val
		default:
			return nil, 0, 0, false, fmt.Errorf("schema_version must be an integer")
		}
	} else {
		// Default to 1 if schema_version is missing
		fromVer = 1
	}

	if fromVer > targetVer {
		return nil, fromVer, 0, false, fmt.Errorf("config was written by a newer Boatstack; update Boatstack")
	}

	if fromVer == targetVer {
		return raw, fromVer, fromVer, false, nil
	}

	currentVer := fromVer
	data := partial

	for currentVer < targetVer {
		var found *configMigration
		for i := range configMigrations {
			if configMigrations[i].from == currentVer {
				found = &configMigrations[i]
				break
			}
		}

		if found == nil {
			return nil, fromVer, 0, false, fmt.Errorf("no migration found from version %d to %d", currentVer, currentVer+1)
		}

		var err error
		data, err = found.apply(data)
		if err != nil {
			return nil, fromVer, 0, false, fmt.Errorf("failed to apply migration from %d to %d: %w", found.from, found.to, err)
		}

		if found.to <= currentVer {
			return nil, fromVer, 0, false, fmt.Errorf("invalid migration path from %d to %d", found.from, found.to)
		}

		currentVer = found.to
	}

	data["schema_version"] = targetVer

	upgraded, err = json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fromVer, 0, false, fmt.Errorf("failed to marshal migrated config: %w", err)
	}
	upgraded = append(upgraded, '\n')

	return upgraded, fromVer, targetVer, true, nil
}
