# Boatstack V1 authority inventory

Frozen against `c5b5e10cdcf4d97b645d705cb164e762acf93ff1`. This file is deletion evidence, not a compatibility contract.

The inventory uses conservative syntactic definitions so its counts are reproducible:

- **Direct lifecycle/completion decision declarations:** every function or method declaration in the nine files named by the V2 deletion contract as independent lifecycle/completion owners.
- **Supporting control-authority declarations:** every declaration in the additional authority-owning files named by that contract.
- **Direct filesystem mutation sites:** every production call to the listed `os` mutation primitives.
- **External-effect sites:** the generic command boundaries plus explicit Git mutation intents. Read-only Git observations are excluded; the generic boundaries are included because their arguments could request effects.

Counts:

- direct lifecycle/completion decision declarations: **84**
- supporting control-authority declarations: **388**
- direct filesystem mutation sites: **106**
- external-effect dispatch/intent sites: **14**
- conservative effect surface: **120**

All paths and line numbers below refer to the frozen base, not the post-rewrite tree.

## Direct lifecycle/completion decision declarations

```text
boatstack/decision.go:49:func ResolvePlanDecision(input PlanDecisionInput) DecisionResolution {
boatstack/delivery_terminal.go:23:func normalizeDeliveryTerminal(value string) (DeliveryTerminal, bool) {
boatstack/delivery_terminal.go:38:func configuredDeliveryTerminal(repo string) DeliveryTerminal {
boatstack/delivery_terminal.go:51:func resolveDeliveryTerminal(repo, feature string) DeliveryTerminal {
boatstack/delivery_terminal.go:66:func deliveryGoalSnapshot(repo string) string {
boatstack/engagement.go:57:func engagementLeasePath(repo string) (string, error) {
boatstack/engagement.go:65:func dormantEngagement(reason string) EngagementStatus {
boatstack/engagement.go:73:func ResolveEngagement(repoPath string, request EngagementRequest) EngagementStatus {
boatstack/engagement.go:135:func engagementLeaseForState(repo string, state DeliveryState) (engagementLease, bool, error) {
boatstack/engagement.go:164:func syncEngagementLease(repo string, state DeliveryState) error {
boatstack/engagement.go:186:func clearEngagementLease(repo string) error {
boatstack/lifecycle.go:41:func lockPlanSHA256(path string) (string, error) {
boatstack/lifecycle.go:57:func lifecycleStateForSlice(status string) (deliverycontrol.StateID, error) {
boatstack/lifecycle.go:74:func lifecycleFingerprint(snapshot LifecycleSnapshot) (string, error) {
boatstack/lifecycle.go:86:func ResolveLifecycleSnapshot(repoPath, feature string) (LifecycleSnapshot, error) {
boatstack/lifecycle.go:164:func amendmentLifecycleState(state deliverycontrol.StateID) bool {
boatstack/next.go:53:func decorateAutonomyStatus(repo string, status NextStatus) NextStatus {
boatstack/next.go:74:func blockedNextStatus(stage, operation, reason string, ambiguity ...string) NextStatus {
boatstack/next.go:82:func featurePlanCandidates(repo string) ([]string, error) {
boatstack/next.go:120:func orphanedFeatureArtifacts(repo string) ([]string, error) {
boatstack/next.go:143:func nextForDelivery(repo, feature string) (NextStatus, error) {
boatstack/next.go:186:func nextForPublished(repo string, state DeliveryState) NextStatus {
boatstack/next.go:209:func observeVisualPublication(repo, feature string) string {
boatstack/next.go:225:func publishedNextStatus(state DeliveryState, pr publishedPRObservation, terminal DeliveryTerminal, visualPublication string) NextStatus {
boatstack/next.go:285:func completedManagedStates(repo string) ([]DeliveryState, error) {
boatstack/next.go:325:func ResolveNext(repoPath, explicitFeature string) (result NextStatus, resultErr error) {
boatstack/next.go:582:func FormatNextStatus(status NextStatus) string {
boatstack/next.go:648:func RenderNextStatusBanner(status NextStatus) string {
boatstack/next.go:671:func bannerRule(title string, width int) string {
boatstack/next.go:681:func bannerSubtitle(status NextStatus) string {
boatstack/next.go:693:func journeyNodes(status NextStatus) []string {
boatstack/next.go:722:func stagePosition(stage string) int {
boatstack/next.go:737:func bannerBlocked(status NextStatus) bool {
boatstack/next.go:745:func friendlyPhrase(status NextStatus) string {
boatstack/next.go:777:func friendlyBlockReason(status NextStatus) string {
boatstack/pr_phase.go:66:func summarizeCheckRollup(entries []prStatusCheck) prCheckSummary {
boatstack/pr_phase.go:93:func classifyStatusCheck(entry prStatusCheck) string {
boatstack/pr_phase.go:133:func derivePRPhase(prState string, checks prCheckSummary, reviewDecision, mergeState string) PRPhase {
boatstack/run.go:31:func blockedRunPreflight(base, head, upstream, relation, reason string) RunPreflight {
boatstack/run.go:41:func blockedRunPreflightWithAuthority(base, head, upstream, relation, reason, authorityStatus, authorityReason string) RunPreflight {
boatstack/run.go:48:func runBranches(repo, explicitFeature string) (string, string, error) {
boatstack/run.go:112:func CheckInstallationPreflight(repoPath string) RunPreflight {
boatstack/run.go:144:func CheckRunPreflight(repoPath, explicitFeature string) RunPreflight {
boatstack/workspace.go:41:func resolveWorkspace(workspace Workspace) ResolvedWorkspace {
boatstack/workspace.go:66:func workspaceEnabled(repo string) bool {
boatstack/workspace.go:77:func reapEnabled(repo string) bool {
boatstack/workspace.go:89:func needsFreshCut(repo, feature string) bool {
boatstack/workspace.go:101:func isMainWorktree(repo string) bool {
boatstack/workspace.go:122:func guardManagedActivationWorktree(repo string, config ProjectConfig, feature string) error {
boatstack/workspace.go:148:func loadWorkspacePolicy(repo string) (ResolvedWorkspace, error) {
boatstack/workspace.go:158:func branchForFeature(feature string) string {
boatstack/workspace.go:192:func blockedCut(reason string) WorkspaceCut {
boatstack/workspace.go:205:func rollbackWorkspaceTransition(repo, branch, worktreePath string, transition workspaceTransition) {
boatstack/workspace.go:224:func featurePackageFingerprint(repo, directory string) (string, error) {
boatstack/workspace.go:236:func featurePackageDigest(directory string) (string, error) {
boatstack/workspace.go:275:func copyFeaturePackage(source, destination string) error {
boatstack/workspace.go:319:func dirtyOutsideFeature(repo, feature string) (bool, error) {
boatstack/workspace.go:346:func transferFeaturePackage(sourceRepo, destinationRepo, feature string, controllerMode SupervisionMode) (string, error) {
boatstack/workspace.go:407:func CutFeatureWorkspace(options WorkspaceCutOptions) (WorkspaceCut, error) {
boatstack/workspace.go:609:func workspaceFeatureForBranch(branch string) string {
boatstack/workspace.go:617:func workspaceBranchLanded(repo, branch, base string) bool {
boatstack/workspace.go:629:func managedWorkspaceLifecycle(repo, branch, base string) (workspaceLifecycleAssessment, bool) {
boatstack/workspace.go:701:func assessWorkspaceLifecycle(repo, branch, base string, abandoned bool) workspaceLifecycleAssessment {
boatstack/workspace.go:740:func (assessment workspaceLifecycleAssessment) cleanupEligible(cleanupAfter string) bool {
boatstack/workspace.go:753:func workspaceMergeStatus(repo, branch, base string) (bool, string) {
boatstack/workspace.go:760:func worktreePathForBranch(repo, branch string) string {
boatstack/workspace.go:779:func branchExists(repo, branch string) bool {
boatstack/workspace.go:805:func blockedCleanup(branch, reason string) WorkspaceCleanup {
boatstack/workspace.go:825:func planWorkspaceRemoval(repo, base, branch, worktreePath, cleanupAfter string, merged, force bool) workspaceRemovalPlan {
boatstack/workspace.go:855:func performWorkspaceRemoval(repo, branch, worktreePath string, merged, force bool) (worktreeRemoved, branchDeleted bool, reason string, err error) {
boatstack/workspace.go:883:func CleanupFeatureWorkspace(options WorkspaceCleanupOptions) (WorkspaceCleanup, error) {
boatstack/workspace.go:971:func boatstackWorktrees(repo string) []worktreeEntry {
boatstack/workspace.go:1028:func blockedReap(reason string) WorkspaceReap {
boatstack/workspace.go:1036:func samePath(a, b string) bool {
boatstack/workspace.go:1050:func reclaimableScan(repo, base, cleanupAfter string, ignored []string) (skipped, reapable []WorkspaceReapItem) {
boatstack/workspace.go:1093:func CountReclaimableWorkspaces(repoPath string) int {
boatstack/workspace.go:1115:func ReapWorkspaces(options WorkspaceReapOptions) (WorkspaceReap, error) {
boatstack/workspace.go:1219:func FeatureWorkspaceStatus(repoPath, branch string) (WorkspaceStatus, error) {
boatstack/workspace_sync.go:37:func blockedWorkspaceSync(result WorkspaceSync, reason string) WorkspaceSync {
boatstack/workspace_sync.go:44:func normalizeRemoteSource(repo, source string) (string, string, string, error) {
boatstack/workspace_sync.go:62:func activeDeliveryOwningBranch(repo, branch string) (string, error) {
boatstack/workspace_sync.go:96:func syncRecoveryRefs(branch, oldCommit string) (string, string) {
boatstack/workspace_sync.go:103:func rollbackWorkspaceCheckpoint(worktreePath, recoveryRef string, checkpointCreated bool) {
boatstack/workspace_sync.go:114:func SyncWorkspace(options WorkspaceSyncOptions) (WorkspaceSync, error) {
```
## Supporting control-authority declarations

```text
boatstack/activation.go:15:func containsEngagementHook(value any) bool {
boatstack/activation.go:38:func detachedHelperPath(repo string) string {
boatstack/activation.go:48:func engagementDesiredEntry(host, event, helper string) map[string]any {
boatstack/activation.go:86:func userHostConfigPath(host string) (string, error) {
boatstack/activation.go:112:func engagementProbeCommand(host, helper string) string {
boatstack/activation.go:120:func engagementProbePowerShellCommand(host, helper string) string {
boatstack/activation.go:131:func engagementHostFragment(host, helper string) ([]byte, error) {
boatstack/activation.go:145:func overrideHookCommands(entry map[string]any, command, commandWindows string) {
boatstack/activation.go:163:func DetachedActivationPlan(repoPath string, hosts []string) (ActivationPlan, error) {
boatstack/activation.go:228:func blockedEngagementActivation(reason string) EngagementActivationResult {
boatstack/activation.go:232:func defaultActivationHosts(hosts []string) []string {
boatstack/activation.go:243:func mergeEngagementHooks(config map[string]any, host, helper string) error {
boatstack/activation.go:278:func removeEngagementHooks(config map[string]any, host string) bool {
boatstack/activation.go:309:func InstallEngagementProbes(repoPath string, hosts []string) (EngagementActivationResult, error) {
boatstack/activation.go:355:func RemoveEngagementProbes(repoPath string, hosts []string) (EngagementActivationResult, error) {
boatstack/authority.go:68:func AuthorityReceiptSigningBytes(receipt AuthorityBoundaryReceipt) ([]byte, error) {
boatstack/authority.go:73:func ResolveAuthorityContext(repoInput string) (AuthorityContext, error) {
boatstack/authority.go:97:func normalizedAuthorityMode(policy *ExternalAuthorityPolicy) string {
boatstack/authority.go:104:func validateExternalAuthorityPolicy(policy *ExternalAuthorityPolicy) error {
boatstack/authority.go:115:func ownerID(info os.FileInfo) (uint64, bool) {
boatstack/authority.go:136:func protectedExternalTrustStore(path string) error {
boatstack/authority.go:161:func loadExternalTrustStore(policy *ExternalAuthorityPolicy) (map[string]string, error) {
boatstack/authority.go:187:func verifyAuthorityBoundary(repo string, policy *ExternalAuthorityPolicy) (string, string) {
boatstack/bootstrap.go:61:func normalizedPlanningDocument(document []byte) ([]byte, error) {
boatstack/bootstrap.go:73:func bootstrapFeatureDisposition(repo string, workspace WorkspaceContext, feature string) (string, *LifecycleSnapshot, error) {
boatstack/bootstrap.go:129:func bootstrapProgram(workspace WorkspaceContext, shell BootstrapShell) string {
boatstack/bootstrap.go:136:func planningArgv(program, repo, feature, artifact, sourcePlan, sourceSHA string, lifecycle *LifecycleSnapshot) []string {
boatstack/bootstrap.go:155:func posixPlanningEnvelopeFor(argv []string, document []byte) string {
boatstack/bootstrap.go:164:func powerShellPlanningWord(value string) string {
boatstack/bootstrap.go:168:func powerShellPlanningEnvelopeFor(argv []string, document []byte) (string, error) {
boatstack/bootstrap.go:189:func ResolvePlanningBootstrap(options BootstrapOptions) (BootstrapPrescription, error) {
boatstack/config_mutation.go:18:func commitDetachedConfigBinding(topology ConfigurationTopology, raw []byte) error {
boatstack/config_mutation.go:42:func MigrateManagedConfiguration(repoPath, requestedTarget string, check bool) (ConfigMigrationResult, error) {
boatstack/config_mutation.go:147:func configFromBytes(path string, raw []byte) (ProjectConfig, error) {
boatstack/config_rebind.go:58:func previewConfigRebind(opts ConfigRebindOptions) (configRebindPreview, error) {
boatstack/config_rebind.go:187:func snapshotFiles(paths []string) ([]savedFile, error) {
boatstack/config_rebind.go:215:func restoreFiles(saved []savedFile) error {
boatstack/config_rebind.go:231:func ConfigRebind(opts ConfigRebindOptions) (result ConfigRebindResult, returnErr error) {
boatstack/config_topology.go:43:func repositorySourceConfigPath(repo string) string {
boatstack/config_topology.go:47:func repositoryPackagePresent(repo string) bool {
boatstack/config_topology.go:51:func fileSHAIfRegular(path string) (string, error) {
boatstack/config_topology.go:65:func detachedAliases(stateRoot, repoID string) ([]string, error) {
boatstack/config_topology.go:80:func ResolveConfigurationTopology(repoPath string) (ConfigurationTopology, error) {
boatstack/config_topology.go:152:func RequireManagedConfiguration(repo string) (ConfigurationTopology, error) {
boatstack/config_topology.go:166:func ValidateConfigurationExport(repoPath, configPath string, write bool) error {
boatstack/config_write.go:35:func withConfigurationMutationLock(repo string, apply func() error) error {
boatstack/config_write.go:64:func projectionTransactionPaths(projection configMutationProjection) []string {
boatstack/config_write.go:83:func verifyConfigurationSource(write configSourceWrite) error {
boatstack/config_write.go:103:func mutateManagedConfiguration(repoPath string, mutate func(*ProjectConfig) (bool, error)) (result configMutationResult, returnErr error) {
boatstack/config_write.go:306:func equalProjectConfig(left, right ProjectConfig) bool {
boatstack/context.go:33:func ProjectOperatorContext(repoPath, operation, host string) (OperatorContext, error) {
boatstack/delivery.go:33:func DeliverySliceStatuses() []string {
boatstack/delivery.go:119:func validateDeliveryGatePolicy(config ProjectConfig, gate, status string, changed []string, reviewerIdentity, reviewMethod string) error {
boatstack/delivery.go:180:func deliveryEvidenceGateStatus(value, gate, sliceID string, explicit bool) string {
boatstack/delivery.go:191:func deliveryDefinitions(plan map[string]any) ([]DeliverySlice, error) {
boatstack/delivery.go:302:func deliveryStateDirectory(repo string) (string, error) {
boatstack/delivery.go:310:func deliveryStatePath(repo, feature string) (string, error) {
boatstack/delivery.go:321:func deliveryReceiptPath(repo, feature, sliceID, gate string) (string, error) {
boatstack/delivery.go:332:func saveDeliveryState(repo string, state DeliveryState) error {
boatstack/delivery.go:362:func LoadDeliveryState(repo, feature string) (DeliveryState, error) {
boatstack/delivery.go:397:func initializeDeliveryState(repo, feature, planPath, lockPath string) error {
boatstack/delivery.go:454:func guardReactivationPreservesProgress(repo, feature, planPath string) error {
boatstack/delivery.go:473:func equalStrings(a, b []string) bool {
boatstack/delivery.go:489:func deliveryDefinitionMatches(a, b DeliverySlice) bool {
boatstack/delivery.go:503:func validateAmendmentPreservesProgress(existing DeliveryState, newSlices []DeliverySlice) error {
boatstack/delivery.go:529:func reconcileAmendedDeliveryState(existing DeliveryState, newSlices []DeliverySlice, lockHash string) DeliveryState {
boatstack/delivery.go:558:func archiveDeliveryReceipt(repo, feature, sliceID, gate, observationID string) (string, error) {
boatstack/delivery.go:580:func appendChangeObservation(repo string, observation ChangeObservation) error {
boatstack/delivery.go:597:func nextChangeObservationID(repo, feature string, fallback int) string {
boatstack/delivery.go:614:func RecordChangeObservation(options ChangeObservationOptions) (ChangeObservation, DeliveryState, error) {
boatstack/delivery.go:824:func activeDeliverySlice(state DeliveryState) (DeliverySlice, error) {
boatstack/delivery.go:834:func isTerminalPRState(prState string) bool {
boatstack/delivery.go:856:func resolveAddressableSlice(state DeliveryState, sliceID string) (int, DeliverySlice, error) {
boatstack/delivery.go:906:func resolveAddressableSliceByBranch(state DeliveryState, branch string) (int, DeliverySlice, bool) {
boatstack/delivery.go:925:func checkDeliveryPlanLock(repo, feature string, state DeliveryState) error {
boatstack/delivery.go:937:func CurrentDeliveryState(repoPath, feature string) (DeliveryState, error) {
boatstack/delivery.go:952:func currentDiffIdentity(repo, base, previewPath string) (string, string, string, []string, error) {
boatstack/delivery.go:979:func pathMatchesDeliveryScope(path string, patterns []string) bool {
boatstack/delivery.go:1004:func validateDeliveryScope(feature string, slice DeliverySlice, changed []string) error {
boatstack/delivery.go:1024:func readDeliveryReceipt(repo, feature, sliceID, gate string) (DeliveryGateReceipt, error) {
boatstack/delivery.go:1043:func RecordDeliveryGate(options DeliveryGateOptions) (DeliveryGateReceipt, error) {
boatstack/delivery.go:1196:func CheckDeliveryReadyForShip(repo, feature, sliceID, base, head, diffHash string, changed []string) (DeliveryState, DeliverySlice, []PRSource, error) {
boatstack/delivery.go:1243:func MarkDeliveryPublished(repo, feature, sliceID, url string) error {
boatstack/delivery.go:1313:func scanManagedDeliveries(repo string) (active []string, invalid []string, err error) {
boatstack/delivery.go:1347:func ActiveManagedDeliveries(repo string) ([]string, error) {
boatstack/delivery.go:1363:func withoutIgnoredDeliveries(features []string, ignored []string) []string {
boatstack/delivery.go:1382:func withoutIgnoredDeliveryStates(states []DeliveryState, ignored []string) []DeliveryState {
boatstack/delivery.go:1402:func IgnoreDelivery(repo, feature string) (bool, error) {
boatstack/delivery.go:1468:func discardOrphanFeatureArtifacts(repo, feature string) (DiscardDeliveryResult, bool, error) {
boatstack/delivery.go:1509:func DiscardDelivery(repoPath, feature string, force bool) (DiscardDeliveryResult, error) {
boatstack/detached.go:39:func detachedStateRoot() (string, error) {
boatstack/detached.go:82:func normalizeOrigin(url string) string {
boatstack/detached.go:100:func firstLine(value string) string {
boatstack/detached.go:113:func repoIdentity(repo string) (RepoIdentity, error) {
boatstack/detached.go:174:func registryPath(stateRoot string) string { return filepath.Join(stateRoot, "registry.json") }
boatstack/detached.go:176:func repositoryControlRoot(stateRoot, repoID string) string {
boatstack/detached.go:180:func bindingPath(stateRoot, repoID string) string {
boatstack/detached.go:184:func loadRegistry(stateRoot string) (detachedRegistry, error) {
boatstack/detached.go:202:func saveRegistry(stateRoot string, registry detachedRegistry) error {
boatstack/detached.go:218:func registerDetachedWorkspaceAlias(sourceRepo, destinationRepo string) (bool, error) {
boatstack/detached.go:269:func unregisterDetachedWorkspaceAlias(repo string) error {
boatstack/detached.go:293:func loadBinding(stateRoot, repoID string) (DetachedBinding, error) {
boatstack/detached.go:309:func bindingMatchesIdentity(binding DetachedBinding, identity RepoIdentity) bool {
boatstack/detached.go:331:func verifyDetachedConfiguration(ctx WorkspaceContext, binding DetachedBinding) error {
boatstack/detached.go:383:func normalizedConfigAuthority(binding DetachedBinding) string {
boatstack/detached.go:399:func detachedContextFor(repo string) (ctx WorkspaceContext, ok bool, err error) {
boatstack/detached.go:446:func detachedContextFromIdentity(stateRoot string, identity RepoIdentity) WorkspaceContext {
boatstack/detached.go:459:func nowRFC3339() string { return operationNow().UTC().Truncate(time.Second).Format(time.RFC3339) }
boatstack/detached.go:465:func RepositoryIsManaged(repo string) bool {
boatstack/flow_control.go:19:func FlowCheck() deliverycontrol.CheckResult {
boatstack/flow_control.go:26:func FormatFlowCheck(result deliverycontrol.CheckResult) string {
boatstack/flow_control.go:53:func flowStateFromStage(stage string) (deliverycontrol.StateID, bool) {
boatstack/flow_control.go:81:func CurrentFlowState(repo, feature string) (deliverycontrol.StateID, bool) {
boatstack/flow_control.go:168:func classifyNextActor(status NextStatus, next FlowNext) NextActor {
boatstack/flow_control.go:300:func posixPlanningWord(value string) string {
boatstack/flow_control.go:316:func powerShellCommandWord(value string) string {
boatstack/flow_control.go:330:func (p PrescribedCommand) commandLineForOS(goos string) string {
boatstack/flow_control.go:386:func (p PrescribedCommand) CommandLine() string {
boatstack/flow_control.go:417:func prescribeCommand(repo, feature string, status NextStatus, transition deliverycontrol.TransitionID) (*PrescribedCommand, bool) {
boatstack/flow_control.go:504:func planningFeatureDir(repo, feature string) string {
boatstack/flow_control.go:508:func prescribePlanning(repo string, status NextStatus) (*PrescribedCommand, string) {
boatstack/flow_control.go:595:func prescribeVisualAttach(repo string, status NextStatus) (*PrescribedCommand, string) {
boatstack/flow_control.go:633:func prescribePostPublish(repo string, status NextStatus, terminal DeliveryTerminal) (*PrescribedCommand, string) {
boatstack/flow_control.go:701:func buildWorkspaceCut(repoArgs []string, feature string) *PrescribedCommand {
boatstack/flow_control.go:709:func buildActivatePlan(featureDir, stage string) *PrescribedCommand {
boatstack/flow_control.go:724:func NextControl(repo, feature string) (FlowNext, error) {
boatstack/flow_control.go:732:func bindFlowCommandPrograms(repo string, next *FlowNext) {
boatstack/flow_control.go:753:func nextControlFromStatus(repo string, status NextStatus) (FlowNext, error) {
boatstack/flow_control.go:828:func FormatFlowNext(next FlowNext) string {
boatstack/flow_control.go:872:func writeAlternatives(b *strings.Builder, alternatives []PrescribedCommand) {
boatstack/flow_control.go:892:func writePrescribed(b *strings.Builder, p *PrescribedCommand) {
boatstack/flow_frontier.go:55:func ResolveFrontier(repoPath string) (FlowFrontier, error) {
boatstack/flow_frontier.go:112:func activeDeliveryRows(repo string, state DeliveryState) []FrontierRow {
boatstack/flow_frontier.go:155:func frontierRowFromStatus(repo string, status NextStatus) FrontierRow {
boatstack/flow_frontier.go:181:func frontierPosition(row FrontierRow) string {
boatstack/flow_frontier.go:189:func FormatFlowFrontier(frontier FlowFrontier) string {
boatstack/flow_frontier.go:223:func frontierLabel(row FrontierRow) string {
boatstack/flow_guard.go:66:func GuardFlowMove(repo, feature string, transition deliverycontrol.TransitionID) FlowGuard {
boatstack/flow_guard.go:93:func GateTransition(gate string) deliverycontrol.TransitionID {
boatstack/operation.go:108:func operationTimestamp() string {
boatstack/operation.go:119:func operationDirectory(repo string) (string, error) {
boatstack/operation.go:132:func pruneLegacyOperationLedger(repo string) {
boatstack/operation.go:141:func operationOwnedPath(repo, operationID string) (controllerPath, error) {
boatstack/operation.go:154:func operationPath(repo, operationID string) (string, error) {
boatstack/operation.go:159:func operationID(kind, target, fingerprint string) string {
boatstack/operation.go:163:func validOperationState(state OperationState) bool {
boatstack/operation.go:172:func validRetryClass(value string) bool {
boatstack/operation.go:181:func validateOperation(receipt OperationReceipt) error {
boatstack/operation.go:194:func loadOperation(repo, id string) (OperationReceipt, error) {
boatstack/operation.go:213:func saveOperation(repo string, receipt OperationReceipt) error {
boatstack/operation.go:228:func withOperationLock(repo, id string, apply func() error) error {
boatstack/operation.go:264:func isLockContention(openErr error, lock string) bool {
boatstack/operation.go:268:func isLockContentionForOS(openErr error, lock, goos string) bool {
boatstack/operation.go:287:func PrepareOperation(options OperationPrepareOptions) (OperationReceipt, error) {
boatstack/operation.go:358:func AuthorizeOperation(repoPath, id, packageFingerprint, authorizationFingerprint string) (OperationReceipt, error) {
boatstack/operation.go:390:func randomLeaseToken() (string, error) {
boatstack/operation.go:398:func BeginOperation(repoPath, id, attemptKey, tool string) (OperationBeginResult, error) {
boatstack/operation.go:476:func reconcileSucceededInstallUpdate(repoPath, id, detail, evidence string) (OperationReceipt, error) {
boatstack/operation.go:510:func completeOperation(repoPath, id, leaseToken, attemptKey, outcome, detail, evidence string, trustedAttempt bool) (OperationReceipt, error) {
boatstack/operation.go:567:func boundedObservation(value string) string {
boatstack/operation.go:579:func CompleteOperation(repo, id, leaseToken, outcome, detail, evidence string) (OperationReceipt, error) {
boatstack/operation.go:583:func CompleteOperationAttempt(repo, id, attemptKey, outcome, detail, evidence string) (OperationReceipt, error) {
boatstack/operation.go:587:func RecordOperationReconciliation(repoPath, id, result, detail, evidence string) (OperationReceipt, error) {
boatstack/operation.go:626:func operationReceipts(repo string) ([]OperationReceipt, error) {
boatstack/operation.go:653:func refreshExpiredOperation(repo, id string) (OperationReceipt, error) {
boatstack/operation.go:678:func ResolveOperationStatus(repoPath, id string) (OperationStatusResult, error) {
boatstack/operation.go:721:func operationStatusFor(receipt OperationReceipt) OperationStatusResult {
boatstack/operation.go:746:func compactOperations(repo string) error {
boatstack/paths.go:75:func newControllerPath(root, target string) (controllerPath, error) {
boatstack/paths.go:86:func (p controllerPath) Validate() error {
boatstack/paths.go:91:func (p controllerPath) Sibling(name string) (controllerPath, error) {
boatstack/paths.go:98:func (w WorkspaceContext) worktreeOwnedPath(target string) (controllerPath, error) {
boatstack/paths.go:109:func (w WorkspaceContext) sharedOwnedPath(target string) (controllerPath, error) {
boatstack/paths.go:126:func WorkspaceFor(repo string) WorkspaceContext {
boatstack/paths.go:149:func ResolveWorkspaceContext(repo string) (WorkspaceContext, error) {
boatstack/paths.go:160:func embeddedWorkspace(repo string) WorkspaceContext {
boatstack/paths.go:164:func pathWithin(root, target string) bool {
boatstack/paths.go:175:func ResolveControllerRepository(path string) (string, error) {
boatstack/paths.go:212:func ResolveControllerRepositoryFor(repoPath, path string) (string, error) {
boatstack/paths.go:234:func invalidateWorkspaceCache() {
boatstack/paths.go:244:func (w WorkspaceContext) configBase() string {
boatstack/paths.go:253:func (w WorkspaceContext) GeneratedRoot() string {
boatstack/paths.go:258:func (w WorkspaceContext) HelperPath() string {
boatstack/paths.go:265:func (w WorkspaceContext) LauncherPath(powerShell bool) string {
boatstack/paths.go:276:func projectLocalLauncherCommand() string {
boatstack/paths.go:283:func (w WorkspaceContext) ExportRoot() string {
boatstack/paths.go:290:func (w WorkspaceContext) FeatureRoot() string {
boatstack/paths.go:296:func (w WorkspaceContext) FeatureDir(feature string) string {
boatstack/paths.go:305:func (w WorkspaceContext) ProjectConfigPath() string {
boatstack/paths.go:311:func (w WorkspaceContext) SourceConfigPath() string {
boatstack/paths.go:318:func (w WorkspaceContext) HostActivationRoot() string {
boatstack/paths.go:325:func (w WorkspaceContext) worktreeControlDir() (string, error) {
boatstack/paths.go:338:func (w WorkspaceContext) sharedControlDir() (string, error) {
boatstack/paths.go:350:func (w WorkspaceContext) DeliveryDir() (string, error) {
boatstack/paths.go:360:func (w WorkspaceContext) OperationDir() (string, error) {
boatstack/paths.go:369:func (w WorkspaceContext) FlowDir() (string, error) {
boatstack/paths.go:381:func (w WorkspaceContext) InsightDir() (string, error) {
boatstack/paths.go:389:func (w WorkspaceContext) GuardDir() (string, error) {
boatstack/paths.go:399:func (w WorkspaceContext) RuntimeDir(version, sourceCommit string) (string, error) {
boatstack/paths.go:419:func (w WorkspaceContext) BootstrapRuntimeDir(version, sourceCommit string) (string, error) {
boatstack/plan.go:22:func stringValue(value any) string {
boatstack/plan.go:27:func stringSlice(value any) ([]string, bool) {
boatstack/plan.go:43:func objectSlice(value any) ([]map[string]any, bool) {
boatstack/plan.go:59:func validationSlice(value any) ([]map[string]any, bool) {
boatstack/plan.go:84:func validateJourneyEvidence(plan map[string]any, version float64) error {
boatstack/plan.go:149:func fencedJSONBlocks(value string) ([]string, error) {
boatstack/plan.go:177:func markedJSON(value, label, startMarker, endMarker string, allowLegacy bool) ([]byte, error) {
boatstack/plan.go:211:func loadJSONObject(path, label, startMarker, endMarker string, allowLegacyMarkdown bool) (map[string]any, error) {
boatstack/plan.go:230:func LoadPlan(path string) (map[string]any, error) {
boatstack/plan.go:237:func CheckSourcePlan(path string) error {
boatstack/plan.go:267:func DiscoverSourcePlan(repo, explicit string) (string, error) {
boatstack/plan.go:290:func sourcePlanForStructuredPlan(planPath, repo string) (string, error) {
boatstack/plan.go:326:func SourcePlanForStructuredPlan(planPath string) (string, error) {
boatstack/plan.go:330:func SpecForStructuredPlan(planPath string) (string, error) {
boatstack/plan.go:345:func checkNonEmptyFile(path, label string) error {
boatstack/plan.go:371:func checkPlanForRepository(repoRoot, planPath string) (PlanCheck, error) {
boatstack/plan.go:428:func CheckPlan(planPath string) (PlanCheck, error) {
boatstack/plan.go:441:func CheckPlanForRepository(repoPath, planPath string) (PlanCheck, error) {
boatstack/plan.go:449:func checkApprovalSourcePlan(options ApprovalOptions) error {
boatstack/plan.go:476:func ValidatePlan(plan map[string]any, opts *ValidatePlanOptions) error {
boatstack/plan.go:645:func taskSafetyText(task map[string]any) string {
boatstack/plan.go:658:func taskHasExternalWrite(task map[string]any) bool {
boatstack/plan.go:671:func destructiveRollback(value string) bool {
boatstack/plan.go:689:func validateTaskSafety(task map[string]any) error {
boatstack/plan.go:735:func CompilePlan(plan map[string]any, opts *ValidatePlanOptions) (map[string]any, map[string]any, string, error) {
boatstack/plan.go:822:func CompilePlanFiles(planPath, outDir string) error {
boatstack/plan.go:831:func canonicalizeExistingAncestor(path string) string {
boatstack/plan.go:850:func compilePlanFiles(planPath, outDir, structuredPlanStatus string) error {
boatstack/plan.go:894:func compileArtifacts(repoRoot, planPath, outDir, structuredPlanStatus string) (compiledArtifacts, error) {
boatstack/plan.go:1033:func LoadApprovalReceipt(path string) (ApprovalReceipt, error) {
boatstack/plan.go:1095:func intValue(value any) int {
boatstack/plan.go:1103:func checkApprovalReceipt(path string, planCheck PlanCheck, repo string) (ApprovalReceipt, error) {
boatstack/plan.go:1144:func CheckApprovalReceipt(path string, planCheck PlanCheck) (ApprovalReceipt, error) {
boatstack/plan.go:1158:func ActivatePlan(options ActivationOptions) error {
boatstack/plan.go:1342:func activationMutation(repoRoot string, options ActivationOptions, structuredPlanStatus string, approval ApprovalOptions) (MutationSet, error) {
boatstack/plan.go:1392:func gitCommit(directory string) string {
boatstack/plan.go:1405:func buildApprovalLock(options ApprovalOptions, tasksSHA256 string) ([]byte, error) {
boatstack/plan.go:1490:func CreateApprovalLock(options ApprovalOptions) error {
boatstack/plan.go:1505:func CheckApprovalLock(options ApprovalOptions) error {
boatstack/planning.go:34:func planningArtifactNames() []string {
boatstack/planning.go:77:func relativeBaselineExclusions(repo string, paths ...string) map[string]bool {
boatstack/planning.go:97:func productBaseline(repo string, artifactPaths ...string) (PlanningBaseline, error) {
boatstack/planning.go:176:func PlanningBaselineForPlan(planPath string) (PlanningBaseline, error) {
boatstack/planning.go:184:func PlanningBaselineForRepository(repoPath, planPath string) (PlanningBaseline, error) {
boatstack/planning.go:196:func rejectSymlinkComponents(root, target string) error {
boatstack/planning.go:218:func atomicWrite(path string, content []byte) error {
boatstack/planning.go:247:func WritePlanningArtifact(options PlanningWriteOptions) (string, error) {
boatstack/planning.go:352:func normalizePlanningTransportBytes(content []byte) []byte {
boatstack/planning.go:357:func RecordApproval(options ApprovalRecordOptions) error {
boatstack/planning.go:488:func CheckInstallationHealth(repoPath string) error {
boatstack/planning.go:579:func Doctor(repoPath string) error {
boatstack/planning.go:593:func DoctorHookHosts(repoPath string) ([]string, error) {
boatstack/planning.go:611:func DoctorRepairHint(err error) error {
boatstack/pr.go:101:func planVisualDecision(repo, feature string) (string, string, []PRVisualScenario, error) {
boatstack/pr.go:136:func ensureCurrentPRVisualEvidence(repo string, config ProjectConfig, mode, feature, base, diffHash string, runner CaptureRunner) (string, error) {
boatstack/pr.go:189:func currentVisualEvidenceIdentity(scenarios []PRVisualScenario, config ProjectConfig) (string, string, error) {
boatstack/pr.go:205:func visualScenarioDefinitionHash(scenarios []PRVisualScenario) (string, error) {
boatstack/pr.go:215:func boundedCaptureDetail(detail string) string {
boatstack/pr.go:223:func resolvePRVisualEvidence(repo string, config ProjectConfig, mode, feature, head, diffHash string) (string, string, int, string, string, string, string, *PRVisualEvidenceManifest, error) {
boatstack/pr.go:313:func publishPRVisualEvidence(repo, prURL string, context PRContext, publisher PRVisualEvidencePublisher) error {
boatstack/pr.go:332:func attachVisualEvidence(repo, prURL string, manifest PRVisualEvidenceManifest, publisher PRVisualEvidencePublisher, policy string) error {
boatstack/pr.go:369:func RetryVisualAttachment(repo, feature string, publisher PRVisualEvidencePublisher) (PRVisualEvidenceManifest, error) {
boatstack/pr.go:402:func gitCommand(repo string, arguments ...string) (string, error) {
boatstack/pr.go:406:func defaultPRBase(repo string) string {
boatstack/pr.go:423:func canonicalPRBaseName(value string) (string, error) {
boatstack/pr.go:437:func canonicalPRBase(repo, value string) (string, error) {
boatstack/pr.go:448:func resolveBaseCommit(repo, base string) (string, error) {
boatstack/pr.go:461:func resolveFetchedOriginBaseCommit(repo, base string) (string, error) {
boatstack/pr.go:473:func previewSlug(branch string) string {
boatstack/pr.go:489:func expectedPRPreviewPath(mode, feature, head string) (string, error) {
boatstack/pr.go:507:func dirtyPaths(repo string) ([]string, error) {
boatstack/pr.go:533:func productDiff(repo, baseCommit, previewPath string) ([]byte, []string, error) {
boatstack/pr.go:566:func productDiffStat(repo, baseCommit string) (string, error) {
boatstack/pr.go:572:func highRiskChangedFiles(changed, patterns []string) []string {
boatstack/pr.go:596:func evidenceGateStatus(value, gate string) string {
boatstack/pr.go:610:func relativeSource(repo, path, kind string) (PRSource, error) {
boatstack/pr.go:629:func featureArtifactPath(directory string, candidates ...string) string {
boatstack/pr.go:648:func featureEvidencePath(featureDir string) string {
boatstack/pr.go:652:func managedPRSources(repo, feature string) ([]PRSource, map[string]string, error) {
boatstack/pr.go:760:func PreparePRContext(options PRContextOptions) (PRContext, error) {
boatstack/pr.go:951:func parsePRFrontmatter(value string) (map[string]string, string, error) {
boatstack/pr.go:1000:func validateVisualEvidenceSection(body, status string, count int) error {
boatstack/pr.go:1029:func section(value, heading string) string {
boatstack/pr.go:1041:func validateEvidenceTable(body string, mode string) error {
boatstack/pr.go:1075:func validateManagedEvidenceSources(body string, sources []PRSource) error {
boatstack/pr.go:1110:func ParsePRPreview(path string) (PRPreview, error) {
boatstack/pr.go:1198:func CheckPRPreview(repoPath, previewPath string) (PRPreview, PRContext, error) {
boatstack/pr.go:1262:func ghAvailable(repo string) error {
boatstack/pr.go:1272:func existingPRURL(repo string) (string, bool, error) {
boatstack/pr.go:1294:func RecommendedPRAction(repo string) (string, string, error) {
boatstack/pr.go:1309:func revalidatePRVisualPrivacy(repo string, context PRContext) error {
boatstack/pr.go:1330:func PublishPR(options PRPublishOptions) (string, error) {
boatstack/pr.go:1511:func extractSystemicBoundaries(repo, feature string) error {
boatstack/pr.go:1544:func PRPreviewTemplate(context PRContext) string {
boatstack/pr.go:1597:func PRContextJSON(context PRContext) ([]byte, error) {
boatstack/pr.go:1601:func PRBody(preview PRPreview) []byte {
boatstack/readiness.go:23:func readinessFingerprint(receipt ReadinessReceipt) (string, error) {
boatstack/readiness.go:37:func checkPlanReadiness(repo, planPath string) (ReadinessReceipt, error) {
boatstack/readiness.go:90:func CheckPlanReadiness(planPath string) (ReadinessReceipt, error) {
boatstack/readiness.go:98:func CheckPlanReadinessForRepository(repoPath, planPath string) (ReadinessReceipt, error) {
boatstack/readiness.go:106:func checkJourneyCapabilities(repo string, plan map[string]any) error {
boatstack/recovery.go:82:func blockedRecovery(reason string, blockers ...string) RecoveryStatus {
boatstack/recovery.go:97:func allManagedDeliveryStates(repo string) (states []DeliveryState, invalid []string, err error) {
boatstack/recovery.go:126:func deliveryBranchAndSlice(state DeliveryState) (string, string, string) {
boatstack/recovery.go:138:func stateMatchesBranch(state DeliveryState, branch string) bool {
boatstack/recovery.go:156:func selectRecoveryDelivery(states []DeliveryState, explicitFeature, currentBranch string) (DeliveryState, []string, error) {
boatstack/recovery.go:198:func observePublishedPR(repo string, state DeliveryState) publishedPRObservation {
boatstack/recovery.go:208:func observePRTarget(repo, prURL, branch string) publishedPRObservation {
boatstack/recovery.go:277:func persistObservedTerminalPRState(repo string, state DeliveryState, observation publishedPRObservation) {
boatstack/recovery.go:303:func suggestedCorrectionFeature(states []DeliveryState, parent string) string {
boatstack/recovery.go:322:func existingRecoveryDiff(repo string, state DeliveryState) (string, []string) {
boatstack/recovery.go:387:func ResolveRecovery(options RecoveryStatusOptions) (RecoveryStatus, error) {
boatstack/recovery.go:532:func refusedRepairState(feature, reason string, blockers ...string) RepairStateResult {
boatstack/recovery.go:545:func RepairState(repoPath, feature string) (RepairStateResult, error) {
boatstack/recovery.go:677:func copyTree(source, destination string) error {
boatstack/runtime_cache.go:30:func helperName() string {
boatstack/runtime_cache.go:38:func platformKey() string { return runtime.GOOS + "-" + runtime.GOARCH }
boatstack/runtime_cache.go:40:func safeCacheSegment(value, label string) (string, error) {
boatstack/runtime_cache.go:49:func gitCommonDir(repo string) (string, error) {
boatstack/runtime_cache.go:74:func worktreeGitDir(repo string) (string, error) {
boatstack/runtime_cache.go:92:func sharedRuntimeDirectory(repo, version, sourceCommit string) (string, error) {
boatstack/runtime_cache.go:96:func sharedRuntimePaths(repo, version, sourceCommit string) (string, string, error) {
boatstack/runtime_cache.go:101:func sharedRuntimeOwnedPaths(repo, version, sourceCommit string) (controllerPath, controllerPath, error) {
boatstack/runtime_cache.go:115:func bootstrapRuntimePaths(repo, version, sourceCommit string) (string, string, error) {
boatstack/runtime_cache.go:120:func bootstrapRuntimeOwnedPaths(repo, version, sourceCommit string) (controllerPath, controllerPath, error) {
boatstack/runtime_cache.go:138:func atomicWriteMode(path string, content []byte, mode fs.FileMode) error {
boatstack/runtime_cache.go:172:func installSharedRuntime(source, repo string, integrations map[string]IntegrationState) (runtimeManifest, error) {
boatstack/runtime_cache.go:186:func installCommandRuntime(source, repo string, integrations map[string]IntegrationState) (runtimeManifest, error) {
boatstack/runtime_cache.go:211:func installDetachedRuntime(repo, source string) (runtimeManifest, error) {
boatstack/runtime_cache.go:228:func writeRuntimeSlot(source string, binaryPath, manifestPath controllerPath, integrations map[string]IntegrationState) (runtimeManifest, error) {
boatstack/runtime_cache.go:272:func loadSharedRuntime(repo string) (runtimeManifest, string, error) {
boatstack/runtime_cache.go:308:func verifyGeneratedRuntime(repo string) error {
boatstack/runtime_cache.go:325:func acquireHydrationLock(repo string) (func(), error) {
boatstack/runtime_cache.go:364:func HydrateWorktree(repoPath string) error {
boatstack/runtime_cache.go:422:func RunHydrateRuntime(repoPath string) error {
boatstack/runtime_cache.go:444:func verifyLocalRuntime(repo string) error {
boatstack/safety.go:69:func (err hookDecodeError) Error() string { return err.code }
boatstack/safety.go:71:func malformedHookInput(code string) error {
boatstack/safety.go:247:func controlledPhaseTransition(command, stage string) bool {
boatstack/safety.go:278:func commandFlagValue(words []string, name string) (string, bool) {
boatstack/safety.go:306:func mergeCommandFeature(current, candidate string) (string, bool) {
boatstack/safety.go:316:func ownedCommandFeature(workspace WorkspaceContext, words []string) (string, bool) {
boatstack/safety.go:346:func ownedReadOnlyHelperCommand(words []string) bool {
boatstack/safety.go:373:func knownOwnedMutationVerb(verb string) bool {
boatstack/safety.go:392:func commandMatchesSolutionVerb(next FlowNext, verb string) bool {
boatstack/safety.go:408:func ownedFlowExecuteCoordinator(words []string, feature string) bool {
boatstack/safety.go:438:func ownedBoatstackCommand(repo, command string) ownedCommandAdmission {
boatstack/safety.go:521:func isPureReadOnlyCommandForRepo(repo, command string) bool {
boatstack/safety.go:532:func controlledWorkspaceSync(repo, command string) bool {
boatstack/safety.go:589:func attemptedRepositoryPath(repo string, input any) string {
boatstack/safety.go:647:func redactContentFields(input any) any {
boatstack/safety.go:674:func fileWriterTool(nameLower, attemptedPath string) bool {
boatstack/safety.go:682:func featureScopedPath(path string) bool {
boatstack/safety.go:690:func featuresPathInCommand(command string) string {
boatstack/safety.go:699:func planningMarkdownPath(path string) bool {
boatstack/safety.go:719:func preActivationFinding(repo, attemptedPath string) (SafetyFinding, bool) {
boatstack/safety.go:738:func publicationBypassFinding(repo, reason, source string) (SafetyFinding, bool) {
boatstack/safety.go:804:func classifySafetyText(value, source string, scanSQL bool) []SafetyFinding {
boatstack/safety.go:835:func isPureReadOnlyCommand(value string) bool {
boatstack/safety.go:853:func shellPipelineStages(value string) ([]string, bool) {
boatstack/safety.go:892:func shellSegments(value string) []string {
boatstack/safety.go:927:func segmentExecutor(segment string) string {
boatstack/safety.go:951:func shellDashCScript(executor, segment string) (string, bool) {
boatstack/safety.go:970:func commandExecutesLiveSQL(command string) bool {
boatstack/safety.go:988:func executedRepositoryFiles(repo, command string) (content []string, symlinks []string) {
boatstack/safety.go:1041:func toolExecutesLiveSQL(name string) bool {
boatstack/safety.go:1045:func ClassifyCommand(repo, command string) []SafetyFinding {
boatstack/safety.go:1135:func ClassifyTool(repo, name string, input any) []SafetyFinding {
boatstack/safety.go:1195:func mutationCapableTool(repo, name string, input any) bool {
boatstack/safety.go:1214:func supervisedToolIdentity(name string, input any) (string, string) {
boatstack/safety.go:1220:func activeManagedOperationScope(repo string) (OperationScope, string, bool) {
boatstack/safety.go:1228:func operationRetryClassForTool(name string) string {
boatstack/safety.go:1239:func hookAttemptKey(host, fingerprint string, eventValue []byte) string {
boatstack/safety.go:1251:func superviseToolAttempt(repo, host, name string, input any, eventValue []byte) *SafetyFinding {
boatstack/safety.go:1302:func postToolEvent(host string, value []byte) (string, any, string, bool, bool) {
boatstack/safety.go:1358:func completeSupervisedToolEvent(repo, host string, value []byte) (bool, bool) {
boatstack/safety.go:1386:func dedupeFindings(values []SafetyFinding) []SafetyFinding {
boatstack/safety.go:1414:func decodeJSONObject(host string, value []byte) (map[string]any, error) {
boatstack/safety.go:1425:func cursorMCPInput(value any) (any, error) {
boatstack/safety.go:1442:func decodeCursorHook(value []byte) (string, any, error) {
boatstack/safety.go:1507:func decodePreToolUseHook(host string, value []byte) (string, any, error) {
boatstack/safety.go:1530:func decodeGeminiHook(value []byte) (string, any, error) {
boatstack/safety.go:1549:func structuredHookDeny(repo, host string, finding SafetyFinding) ([]byte, error) {
boatstack/safety.go:1623:func denialMessage(repo, host string, finding SafetyFinding) string {
boatstack/safety.go:1630:func EngagementProbeDecision(options SafetyHookOptions) ([]byte, bool) {
boatstack/safety.go:1634:func HookDecision(options SafetyHookOptions) ([]byte, bool) {
boatstack/safety.go:1694:func operationalChangedFiles(repo string, highRisk []string, defaultBranch string) ([]string, error) {
boatstack/safety.go:1741:func CheckRepositorySafety(repoPath string) (SafetyReport, error) {
```

## Direct filesystem mutation sites

```text
boatstack/atomic_unix.go:8:	return os.Rename(source, destination)
boatstack/attach.go:139:	if err := os.MkdirAll(ctx.controlRoot, 0o755); err != nil {
boatstack/attach.go:175:	if err := os.MkdirAll(filepath.Dir(bindingPath(stateRoot, identity.RepoID)), 0o755); err != nil {
boatstack/attach.go:263:		if err := os.RemoveAll(repositoryControlRoot(stateRoot, repoID)); err != nil {
boatstack/capture.go:267:	if err := os.MkdirAll(staging, 0o700); err != nil {
boatstack/capture.go:323:		if err := os.Remove(receiptPath); err != nil && !os.IsNotExist(err) {
boatstack/config_rebind.go:219:			if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
boatstack/config_write.go:45:		file, openErr := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
boatstack/config_write.go:49:			defer os.Remove(lock)
boatstack/config_write.go:56:			_ = os.Remove(lock)
boatstack/delivery.go:574:	if err := os.Remove(path); err != nil {
boatstack/delivery.go:1182:			_ = os.Remove(reviewPath)
boatstack/delivery.go:1493:	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
boatstack/delivery.go:1496:	if err := os.Rename(dir, destination); err != nil {
boatstack/delivery.go:1577:	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
boatstack/delivery.go:1581:	if err := os.Rename(featureDir, destination); err != nil {
boatstack/denial_ledger.go:96:	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
boatstack/denial_ledger.go:99:	_ = os.WriteFile(path, value, 0o644)
boatstack/denial_ledger.go:128:	_ = os.Remove(path)
boatstack/detached.go:208:	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
boatstack/detached.go:211:	return os.WriteFile(registryPath(stateRoot), raw, 0o644)
boatstack/detached_migration.go:214:	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
boatstack/detached_migration.go:217:	temporary, err := os.MkdirTemp(filepath.Dir(target), ".boatstack-feature-import-*")
boatstack/detached_migration.go:221:	defer os.RemoveAll(temporary)
boatstack/detached_migration.go:232:			return os.MkdirAll(destination, 0o755)
boatstack/detached_migration.go:256:	return os.Rename(temporary, target)
boatstack/engagement.go:174:		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
boatstack/engagement.go:191:	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
boatstack/export.go:750:			if err := os.Remove(target); err != nil {
boatstack/init.go:657:	if err := os.WriteFile(configPath, rawConfig, 0o644); err != nil {
boatstack/init.go:875:	return os.WriteFile(path, []byte(strings.TrimSpace(text)+"\n"), 0o644)
boatstack/init_transaction.go:17:	backup, err := os.MkdirTemp("", "boatstack-init-rollback-*")
boatstack/init_transaction.go:23:		_ = os.RemoveAll(backup)
boatstack/init_transaction.go:57:			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
boatstack/init_transaction.go:63:			return os.MkdirAll(target, info.Mode().Perm())
boatstack/init_transaction.go:72:		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
boatstack/init_transaction.go:75:		return os.WriteFile(target, value, info.Mode().Perm())
boatstack/init_transaction.go:91:		if err := os.RemoveAll(filepath.Join(snapshot.repo, entry.Name())); err != nil {
boatstack/init_transaction.go:99:	return os.RemoveAll(snapshot.backup)
boatstack/init_transaction.go:106:	if err := os.RemoveAll(snapshot.backup); err != nil {
boatstack/insight.go:532:	if err := os.MkdirAll(root, 0o755); err != nil {
boatstack/insight.go:535:	temporary, err := os.MkdirTemp(root, ".insight-*")
boatstack/insight.go:539:	defer os.RemoveAll(temporary)
boatstack/insight.go:552:	if err := os.Rename(temporary, directory); err != nil {
boatstack/insight.go:640:	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
boatstack/insight.go:644:		file, openErr := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
boatstack/insight.go:648:			defer os.Remove(lock)
boatstack/insight.go:655:			_ = os.Remove(lock)
boatstack/installation_repair.go:455:	if err := os.MkdirAll(directory, 0o700); err != nil {
boatstack/integrations.go:85:		if err := os.MkdirAll(filepath.Dir(installRoot), 0o755); err != nil {
boatstack/internal/deliverycontrol/codinglog.go:25:	if err := os.MkdirAll(dir, 0o755); err != nil {
boatstack/internal/deliverycontrol/codinglog.go:32:	file, err := os.OpenFile(filepath.Join(dir, codingLogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
boatstack/internal/deliverycontrol/commandlog.go:68:	if err := os.MkdirAll(dir, 0o755); err != nil {
boatstack/internal/deliverycontrol/commandlog.go:75:	file, err := os.OpenFile(filepath.Join(dir, commandLogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
boatstack/internal/deliverycontrol/trajectorylog.go:25:	if err := os.MkdirAll(dir, 0o755); err != nil {
boatstack/internal/deliverycontrol/trajectorylog.go:32:	file, err := os.OpenFile(filepath.Join(dir, trajectoryLogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
boatstack/mutation.go:163:	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
boatstack/mutation.go:167:		file, openErr := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
boatstack/mutation.go:171:			defer os.Remove(lock)
boatstack/mutation.go:178:			_ = os.Remove(lock)
boatstack/mutation.go:443:					if rmErr := os.Remove(op.native); rmErr != nil && !os.IsNotExist(rmErr) {
boatstack/mutation.go:527:			_ = os.Remove(native)
boatstack/operation.go:138:	_ = os.RemoveAll(legacy)
boatstack/operation.go:238:	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
boatstack/operation.go:242:		file, openErr := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
boatstack/operation.go:246:			defer os.Remove(lock)
boatstack/operation.go:253:			_ = os.Remove(lock)
boatstack/planning.go:220:	if err := os.MkdirAll(directory, 0o755); err != nil {
boatstack/planning.go:223:	temporary, err := os.CreateTemp(directory, ".boatstack-planning-*")
boatstack/planning.go:228:	defer os.Remove(temporaryPath)
boatstack/pr.go:1450:	temporary, err := os.CreateTemp("", "boatstack-pr-body-*.md")
boatstack/pr.go:1455:	defer os.Remove(temporaryPath)
boatstack/pr.go:1526:	f, err := os.OpenFile(outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
boatstack/recovery.go:650:	if err := os.MkdirAll(destParent, 0o700); err != nil {
boatstack/recovery.go:653:	if err := os.Rename(directory, dest); err != nil {
boatstack/recovery.go:657:		if rmErr := os.RemoveAll(directory); rmErr != nil {
boatstack/recovery.go:688:			return os.MkdirAll(target, 0o755)
boatstack/recovery.go:701:		if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
boatstack/recovery.go:704:		return os.WriteFile(target, data, info.Mode().Perm())
boatstack/runtime.go:265:	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
boatstack/runtime.go:268:	return os.WriteFile(path, value, mode)
boatstack/runtime_cache.go:140:	if err := os.MkdirAll(directory, 0o755); err != nil {
boatstack/runtime_cache.go:148:	temporary, err := os.CreateTemp(directory, ".boatstack-runtime-*")
boatstack/runtime_cache.go:153:	defer os.Remove(temporaryPath)
boatstack/runtime_cache.go:265:		_ = os.Remove(binaryPath.path)
boatstack/runtime_cache.go:266:		_ = os.Remove(manifestPath.path)
boatstack/runtime_cache.go:330:	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
boatstack/runtime_cache.go:334:		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
boatstack/runtime_cache.go:338:				os.Remove(lockPath)
boatstack/runtime_cache.go:342:				os.Remove(lockPath)
boatstack/runtime_cache.go:345:			return func() { _ = os.Remove(lockPath) }, nil
boatstack/runtime_cache.go:356:			_ = os.Remove(lockPath)
boatstack/update_publication.go:364:	temporary, err := os.CreateTemp("", "boatstack-update-pr-*.md")
boatstack/update_publication.go:369:	defer os.Remove(temporaryPath)
boatstack/visual_publisher.go:90:	bodyFile, err := os.CreateTemp("", "boatstack-evidence-comment-*.md")
boatstack/visual_publisher.go:95:	defer os.Remove(bodyPath)
boatstack/workspace.go:277:	if err := os.MkdirAll(parent, 0o755); err != nil {
boatstack/workspace.go:280:	temporary, err := os.MkdirTemp(parent, ".boatstack-workspace-transfer-")
boatstack/workspace.go:284:	defer os.RemoveAll(temporary)
boatstack/workspace.go:302:			return os.MkdirAll(target, info.Mode().Perm())
boatstack/workspace.go:308:		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
boatstack/workspace.go:311:		return os.WriteFile(target, value, info.Mode().Perm())
boatstack/workspace.go:316:	return os.Rename(temporary, destination)
boatstack/workspace.go:392:		_ = os.RemoveAll(destination)
boatstack/workspace.go:397:			_ = os.RemoveAll(destination)
boatstack/workspace.go:523:			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
```

## External-effect dispatch/intent sites

```text
boatstack/capture.go:43:	command := exec.Command("sh", "-c", request.Command)
boatstack/command.go:43:	command := exec.Command(name, arguments...)
boatstack/command.go:56:	command := exec.Command(name, arguments...)
boatstack/hooks.go:64:		command = exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path, "-HostName", host)
boatstack/hooks.go:67:		command = exec.CommandContext(ctx, "bash", path, host)
boatstack/init.go:33:	command := exec.Command("git", append([]string{"-C", repo}, arguments...)...)
boatstack/integrations.go:16:		command := exec.Command(name, arguments...)
boatstack/integrations.go:24:		command := exec.Command(name, arguments...)
boatstack/migrate_effect_grade.go:69:	cmd := exec.Command("sh", "-c", command)
boatstack/pr.go:1447:	if _, err := gitCommand(repo, "push", "--set-upstream", "origin", context.HeadBranch); err != nil {
boatstack/run.go:161:	if _, err := runGitCommand(repo, "fetch", "origin"); err != nil {
boatstack/update_publication.go:354:		if _, err := gitCommand(repo, arguments...); err != nil {
boatstack/update_publication.go:357:		if _, err := gitCommand(repo, "commit", "-m", "chore: update Boatstack to "+preview.Version); err != nil {
boatstack/update_publication.go:361:	if _, err := gitCommand(repo, "push", "--set-upstream", "origin", preview.HeadBranch); err != nil {
```
