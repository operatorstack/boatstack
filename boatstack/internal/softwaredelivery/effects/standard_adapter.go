package effects

import (
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
)

// standardGateName belongs to the trusted StandardFlow native adapter. The
// generic Kernel registry does not infer gate semantics from transition IDs.
func standardGateName(id catalog.TransitionID) (string, bool) {
	value := string(id)
	if !strings.HasPrefix(value, "gate.") || !strings.HasSuffix(value, ".record") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(value, "gate."), ".record")
	return name, name != ""
}
