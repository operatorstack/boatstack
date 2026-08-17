### Preserve durable state during named-role upgrades

Boatstack now promotes the immediately preceding durable state schema 6 to
schema 7 while preserving its planning and control-bundle fingerprints. This
allows existing installations to adopt named human identity roles without
misclassifying valid schema-6 state as an unsupported older version.
