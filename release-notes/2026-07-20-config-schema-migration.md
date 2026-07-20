### Versioned configuration schema migration system

Introduced a non-breaking, versioned configuration migration system for `.boatstack-project.json`. Older configurations are automatically upgraded to the latest schema version during `/boatstack-update`, with full dry-run reporting, schema gap detection, and helpful error messages for newer configuration versions. A generated configuration schema reference document (`CONFIG_SCHEMA.md`) is now embedded and distributed in the product bundle.
