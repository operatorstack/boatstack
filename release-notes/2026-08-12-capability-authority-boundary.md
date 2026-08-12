### Keep program declarations separate from authority

Repository Control Programs now declare a bounded capability surface while the
kernel independently classifies effects and requires matching external
authority. Prescriptions, admissions, effects, and receipts preserve that exact
context, and missing or changed capabilities fail closed.
