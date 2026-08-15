import { all, defineFlow, entry, fact, hostParameter, marked } from "@operatorstack/boatstack";
import {
  inbox,
  planInboxResolver,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  trustedOperators,
  repositoryDefaultBranch,
  trustedTransition,
  type TrustedStep,
} from "@operatorstack/boatstack-software-delivery";

const lifecycle = [
  { id: "gate.test.record", priority: 62 },
  { id: "gate.review.record", priority: 63 },
  { id: "gate.change.record", priority: 64 },
  { id: "publication.preview", priority: 72 },
  { id: "publication.execute", priority: 76 },
  { id: "publication.observe", priority: 77 },
] satisfies TrustedStep[];

export default defineFlow({
  id: "product-delivery-c",
  version: "1",
  declarations: { input_resolvers: [planInboxResolver] },
  facets: softwareDeliveryFacets,
  evidence: softwareDeliveryEvidence,
  operators: trustedOperators(lifecycle),
  transitions: [
    trustedTransition({ id: "gate.test.record", priority: 62 }, { parameters: {
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }),
      evidence_path: hostParameter({ id: "evidence-path", description: "evidence-path", authorities: ["human"], scope: "transition" }),
      evidence_fingerprint: hostParameter({ id: "evidence-fingerprint", description: "evidence-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "gate.review.record", priority: 63 }, { parameters: {
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }),
      evidence_path: hostParameter({ id: "evidence-path", description: "evidence-path", authorities: ["human"], scope: "transition" }),
      evidence_fingerprint: hostParameter({ id: "evidence-fingerprint", description: "evidence-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "gate.change.record", priority: 64 }, { parameters: {
      source_revision: hostParameter({ id: "source-revision", description: "source-revision", authorities: ["human"], scope: "transition" }),
      evidence_path: hostParameter({ id: "evidence-path", description: "evidence-path", authorities: ["human"], scope: "transition" }),
      evidence_fingerprint: hostParameter({ id: "evidence-fingerprint", description: "evidence-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "publication.preview", priority: 72 }, { parameters: {
      base_ref: repositoryDefaultBranch(),
      head_ref: hostParameter({ id: "head-ref", description: "head-ref", authorities: ["human"], scope: "transition" }),
      body_path: hostParameter({ id: "body-path", description: "body-path", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "publication.execute", priority: 76 }, { parameters: {
      preview_fingerprint: hostParameter({ id: "preview-fingerprint", description: "preview-fingerprint", authorities: ["human"], scope: "transition" }),
    }}),
    trustedTransition({ id: "publication.observe", priority: 77 }, { parameters: {
      publication_id: hostParameter({ id: "publication-id", description: "publication-id", authorities: ["human"], scope: "transition" }),
    }}),
  ],
  targets: [marked("published-pr", all(
    fact("verification", ["current"]),
    fact("configuration", ["verified"]),
    fact("runtime", ["verified"]),
    fact("publication", ["open"]),
  ))],
  entries: [entry({
    id: "run",
    target: "published-pr",
    inputs: [inbox(".boatstack/plans/inbox")],
    diagnostics: { explain_on_suspend: true },
  })],
});
