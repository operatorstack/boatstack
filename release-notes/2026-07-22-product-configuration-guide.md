### Make repository policy discoverable without lengthening the README

Boatstack now links from its public README to a complete, value-first configuration guide. Maintainers can start from the delivery outcome they want, then see every supported `.boatstack-project.json` field, accepted value, default, interaction, and focused example without reverse-engineering the generated project file. The canonical internal schema now also covers managed workspaces, boundary analysis, supported adapters, integration metadata, and optional project commands.

A supervisory contract now derives the public configuration surface from the implementation's JSON tags and compares it with both documentation slices. Adding, removing, or renaming a configuration field without updating the guide and canonical schema fails the Go test suite instead of silently creating documentation drift.
