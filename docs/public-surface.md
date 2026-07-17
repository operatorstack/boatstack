<!-- Generated from operatorstack/intelligence-flow. Edit the upstream public source, not this file. -->

# Boatstack public-surface contract

Boatstack's README is a product-builder homepage, not the complete manual. Public presentation may change freely inside this contract while the runtime remains deterministic.

## Reading order

The homepage must answer, in order:

1. What will Boatstack help me achieve?
2. What remains portable when I change tools, models, or skills?
3. How do I install and start it?
4. What will using it feel like?
5. Why do these steps exist?
6. Where can I inspect the details?

Keep the README under 1,500 words. Put equations, schemas, internal helper commands, long artifact examples, and benchmark methodology in linked technical documents.

## Evidence language

Every material capability claim must have a `boatstack-claim:<id>` marker that resolves to `public-claims.json`. Distinguish:

- an observed problem;
- behavior verified in Boatstack tests;
- a product outcome that is still being evaluated.

Do not use “proven,” “optimal,” performance uplift, cost reduction, or safety-improvement language without a matching evaluation. A fixture can verify enforcement behavior; it cannot by itself prove improved product delivery.

## Design review

When Huashu Design is installed, use it to review a public-surface change. The review must still satisfy this portable fallback:

- preserve the stacked-node mark, ink `#0F172A`, and electric blue `#2563EB`;
- design from real Boatstack content rather than a generic AI landing-page pattern;
- use one dominant product journey and progressive disclosure;
- keep portability diagrams subordinate to the product journey and focused on real hosts, models, skills, and repository state;
- add no invented statistics, testimonials, decorative icons, or unsupported badges;
- keep diagrams accessible, readable in light and dark themes, and useful without decoration;
- render and inspect changed SVGs or public pages before approval.

## Upgrade checklist

Any user-facing upgrade must declare:

- the user problem it addresses;
- the observation or requirement behind it;
- its claim-record status and supporting test/evaluation;
- the README or guide that changes, or why no public change is needed.

Internal refactors may declare no public impact. A new public claim without a record, readable explanation, and current status is incomplete.
