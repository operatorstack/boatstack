package surfaces

import "github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"

// ClassifyCommandIntent is the host-facing projection of the kernel's one
// consumer-neutral command classifier.
func ClassifyCommandIntent(command string) supervisor.CommandIntent {
	return supervisor.ClassifyCommandIntent(command)
}
