package skillprojection

// BootstrapContract is shared by every generated Flow entry skill. It covers
// failures that occur before a repository Control Program can be loaded.
func BootstrapContract() string {
	return `Before starting the Flow, verify that the ` + "`boatstack`" + ` command is
available (` + "`command -v boatstack`" + ` on POSIX or ` + "`Get-Command boatstack`" + ` in
PowerShell). If it is absent, read the exact committed
` + "`.boatstack/runtime.json`" + ` regular file. Report
` + "`BOATSTACK_LAUNCHER_NOT_FOUND`" + `, the pinned version and SHA-256, and the
tag-specific installer command for the current platform:

POSIX:
` + "`BOATSTACK_MODE=hydrate BOATSTACK_VERSION=<exact-version> /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/<exact-version>/install.sh)\"`" + `

PowerShell:
` + "`$env:BOATSTACK_MODE='hydrate'; $env:BOATSTACK_VERSION='<exact-version>'; Invoke-RestMethod https://raw.githubusercontent.com/operatorstack/boatstack/<exact-version>/install.ps1 | Invoke-Expression`" + `

Replace ` + "`<exact-version>`" + ` only with the validated release version in the
pin. If the pin is absent or invalid, report
` + "`BOATSTACK_RUNTIME_PIN_MISSING`" + ` or ` + "`BOATSTACK_RUNTIME_PIN_INVALID`" + ` and
stop without guessing a version or selecting ` + "`latest`" + `.

Display the installer command and ask for explicit approval. Never run it or
authorize installation on the user's behalf. A bootstrap failure creates no
Flow run ID. Preserve any Boatstack bootstrap diagnostic verbatim, including
stderr, and resume this same requested entry only after the user has installed
the exact runtime.`
}
