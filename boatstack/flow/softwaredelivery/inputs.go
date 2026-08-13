package softwaredelivery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
)

const PlanInboxResolver = "software-delivery.plan-inbox"

type PlanInbox struct {
	Path        string `json:"path"`
	Cardinality string `json:"cardinality"`
}

func PlanInboxForEntry(entry controlprogram.Entry) (PlanInbox, error) {
	if len(entry.Inputs) != 1 {
		return PlanInbox{}, fmt.Errorf("entry %q requires exactly one plan input", entry.ID)
	}
	input := entry.Inputs[0]
	if input.ID != "plan" || input.Type != "markdown-file" || !input.Required || input.Resolver != PlanInboxResolver {
		return PlanInbox{}, fmt.Errorf("entry %q requires one required markdown plan resolved by %s", entry.ID, PlanInboxResolver)
	}
	decoder := json.NewDecoder(bytes.NewReader(input.Config))
	decoder.DisallowUnknownFields()
	var config PlanInbox
	if err := decoder.Decode(&config); err != nil {
		return PlanInbox{}, fmt.Errorf("entry %q has invalid plan inbox config: %w", entry.ID, err)
	}
	if err := requireInputEOF(decoder); err != nil || config.Cardinality != "exactly-one" || !safeInputPath(config.Path) {
		return PlanInbox{}, fmt.Errorf("entry %q requires a canonical repository plan inbox with exactly-one cardinality", entry.ID)
	}
	return config, nil
}

func requireInputEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON")
		}
		return err
	}
	return nil
}

func safeInputPath(value string) bool {
	if value == "" || strings.Contains(value, `\`) {
		return false
	}
	platformPath := filepath.FromSlash(value)
	if filepath.IsAbs(platformPath) {
		return false
	}
	clean := filepath.Clean(platformPath)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && filepath.ToSlash(clean) == value
}
