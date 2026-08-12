package durable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

const BindingSchemaVersion = 1

type Binding struct {
	SchemaVersion   int            `json:"schema_version"`
	RepositoryID    string         `json:"repository_id"`
	GitCommonID     string         `json:"git_common_id"`
	Topology        model.Topology `json:"topology"`
	ControllerID    string         `json:"controller_id"`
	ConfigAuthority string         `json:"config_authority"`
	CreatedAt       time.Time      `json:"created_at"`
}

func (b Binding) Validate() error {
	if b.SchemaVersion != BindingSchemaVersion || b.RepositoryID == "" || b.GitCommonID == "" || b.ControllerID == "" || b.ConfigAuthority == "" || b.CreatedAt.IsZero() {
		return fmt.Errorf("binding schema and identity fields are required")
	}
	if b.Topology != model.TopologyDetached && b.Topology != model.TopologyHybrid {
		return fmt.Errorf("external binding requires detached or hybrid topology")
	}
	if b.ConfigAuthority != "repository" && b.ConfigAuthority != "external" {
		return fmt.Errorf("external binding has invalid configuration authority %q", b.ConfigAuthority)
	}
	return nil
}

func EncodeBinding(binding Binding) ([]byte, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	value, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(value, '\n'), nil
}

func DecodeBinding(value []byte) (Binding, error) {
	var binding Binding
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&binding); err != nil {
		return Binding{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Binding{}, fmt.Errorf("binding contains trailing JSON")
	}
	if err := binding.Validate(); err != nil {
		return Binding{}, err
	}
	return binding, nil
}
