package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
)

type commandPattern struct {
	operation string
	pattern   *regexp.Regexp
}

// These are constitutional effect classes, not workflow heuristics. They are
// intentionally repository-independent so every host and managed command
// receives the same result. Raw command text is fingerprinted but never
// persisted in a receipt or process event.
var destructiveCommandPatterns = []commandPattern{
	{"database.reset", regexp.MustCompile(`(?i)\b(?:supabase\s+db\s+reset|prisma\s+migrate\s+reset|rails\s+db:(?:drop|reset)|django-admin\s+flush|manage\.py\s+flush|alembic\s+downgrade\s+base|pg_restore\b[^\n]*\s--clean\b)`)},
	{"external-resource.delete", regexp.MustCompile(`(?i)\bsupabase[\s"',()]+branches?[\s"',()]+delete\b`)},
	{"external-lifecycle.weaken", regexp.MustCompile(`(?i)\bsupabase[\s"',()]+branches?[\s"',()]+update\b[^\n;&|]*(?:--persistent(?:=|[\s"',()]+)false\b|--no-persistent\b)`)},
	{"external-service.expose", regexp.MustCompile(`(?i)(?:\bgcloud\s+run\s+(?:deploy|services\s+update)\b[^\n;&|]*--allow-unauthenticated\b|\bgcloud\s+(?:projects|run\s+services)\s+(?:add-iam-policy-binding|set-iam-policy)\b[^\n;&|]*(?:allusers|roles/run\.invoker)|\bkubectl\s+(?:create|expose|patch|apply)\b[^\n;&|]*(?:type[=:](?:loadbalancer|nodeport)|0\.0\.0\.0/0)|\baws\s+[^\n;&|]*(?:authorize-security-group-ingress|put-public-access-block)\b[^\n;&|]*(?:0\.0\.0\.0/0|block-public-acls\s+false))`)},
	{"filesystem.recursive-delete", regexp.MustCompile(`(?i)\b(?:rm\s+-[^\n;]*(?:r[^\n;]*f|f[^\n;]*r)|remove-item\s+[^\n;]*-recurse[^\n;]*-force)\s+(?:["']?(?:/|~|\$home|\.|\.\.)["']?\s*(?:;|&&|\|\||$)|[^\s;]*\*[^\s;]*)`)},
	{"git.reset-hard", regexp.MustCompile(`(?i)\bgit\s+reset\s+[^\n;&|]*--hard\b`)},
	{"git.clean-force", regexp.MustCompile(`(?i)\bgit\s+clean\s+-[^\s]*(?:f[^\s]*d|d[^\s]*f|x)[^\s]*`)},
	{"git.force-push", regexp.MustCompile(`(?i)\bgit\s+push\b[^\n;&|]*(?:--force(?:-with-lease)?|-f\b)`)},
	{"infrastructure.destroy", regexp.MustCompile(`(?i)\b(?:terraform|tofu|pulumi)\s+destroy\b|\bkubectl\s+delete\s+(?:namespace|cluster|persistentvolume|persistentvolumeclaim|pvc)\b|\bdocker\s+volume\s+(?:rm|prune)\b|\bgcloud\s+(?:projects|sql\s+instances|compute\s+(?:instances|disks))\s+delete\b|\baws\s+[^\n;&|]*(?:delete-cluster|delete-db-instance|terminate-instances|delete-volume|delete-bucket)\b`)},
	{"recovery.destroy", regexp.MustCompile(`(?i)\b(?:delete|remove|disable)\b[^\n;]*(?:backup|snapshot|point-in-time|pitr|recovery)\b`)},
	{"publication.merge", regexp.MustCompile(`(?i)\bgh\s+pr\s+merge\b`)},
	{"external-tool.destructive", regexp.MustCompile(`(?i)\bmcp[_-]{2}[^\s]*(?:delete|drop|destroy|reset|execute[_-]?sql)[^\s]*\b`)},
}

var (
	liveSQLClient  = regexp.MustCompile(`(?i)(?:^|[;&|]\s*|\b(?:sudo|env)\s+)(?:psql|mysql|mariadb|mongo|mongosh|cockroach|sqlcmd|usql|clickhouse-client)\b`)
	destructiveSQL = regexp.MustCompile(`(?i)\b(?:drop\s+(?:database|schema|table)|alter\s+table\b[^;\n]*\bdrop\s+(?:column|constraint)|truncate(?:\s+table)?|delete\s+from\b)`)
	sqlFileRun     = regexp.MustCompile(`(?i)\b(?:psql|mysql|mariadb|cockroach|sqlcmd|usql|clickhouse-client)\b[^\n;&|]*(?:\s-f\s|\s--file(?:=|\s)|\s<\s*)`)
	managedState   = regexp.MustCompile(`(?i)(?:^|[\s"'=])(?:\.git[/\\](?:worktrees[/\\][^/\\]+[/\\])?boatstack(?:[/\\]|$)|\.boatstack[/\\](?:approvals|evidence|plans|publication)(?:[/\\]|$)|\.boatstack[/\\]project\.json\b)`)
	mutationSyntax = regexp.MustCompile(`(?i)(?:^|[;&|]\s*)(?:rm|mv|cp|touch|mkdir|tee|truncate|install|remove-item|move-item|copy-item|new-item|set-content|add-content)\b|\bsed\s+-[^\s]*i\b|(?:^|[^<])>{1,2}[^>]`)
	dataOnlyPrefix = regexp.MustCompile(`(?i)^(?:rg|grep|cat|head|tail|less|wc|awk|sort|uniq|cut|tr|jq|column|nl|comm|rev|fold|git\s+(?:add|commit|diff|status|show|log|grep|restore\s+--staged))\b`)
)

var managedCommandPatterns = []struct {
	pattern    *regexp.Regexp
	operation  string
	transition catalog.TransitionID
}{
	{regexp.MustCompile(`(?i)\bgit\s+worktree\s+remove\b`), "workspace.remove", "workspace.cleanup"},
	{regexp.MustCompile(`(?i)\bgh\s+pr\s+create\b`), "publication.create", "publication.execute"},
	{regexp.MustCompile(`(?i)\bgh\s+pr\s+edit\b`), "publication.edit", "publication.correct"},
	{regexp.MustCompile(`(?i)\bgh\s+pr\s+ready\b`), "publication.ready", "publication.correct"},
	{regexp.MustCompile(`(?i)\bgh\s+api\b[^\n;&|]*(?:/pulls\b|/pull-requests\b)[^\n;&|]*(?:\s-X\s*(?:POST|PATCH)|--request\s+(?:POST|PATCH))`), "publication.api-write", "publication.correct"},
	{regexp.MustCompile(`(?i)\bgit\s+push\b`), "publication.push", "publication.execute"},
}

func ClassifyCommandIntent(command string) CommandIntent {
	normalized := strings.Join(strings.Fields(command), " ")
	digest := sha256.Sum256([]byte(command))
	intent := CommandIntent{Class: IntentOrdinary, Operation: "repository.command", Fingerprint: hex.EncodeToString(digest[:])}
	if dataOnlyPrefix.MatchString(normalized) && !strings.ContainsAny(normalized, ";|<>") && !strings.Contains(normalized, "&&") && !strings.Contains(normalized, "$(") && !strings.Contains(normalized, "`") {
		return intent
	}
	for _, candidate := range destructiveCommandPatterns {
		if candidate.pattern.MatchString(normalized) {
			intent.Class, intent.Operation = IntentDestructive, candidate.operation
			return intent
		}
	}
	if (liveSQLClient.MatchString(normalized) && destructiveSQL.MatchString(normalized)) || sqlFileRun.MatchString(normalized) {
		intent.Class, intent.Operation = IntentDestructive, "database.destruction"
		return intent
	}
	if managedState.MatchString(normalized) && mutationSyntax.MatchString(normalized) {
		intent.Class, intent.Operation = IntentDestructive, "managed-state.tamper"
		return intent
	}
	for _, candidate := range managedCommandPatterns {
		if candidate.pattern.MatchString(normalized) {
			intent.Class, intent.Operation, intent.Transition = IntentManagedBypass, candidate.operation, candidate.transition
			return intent
		}
	}
	return intent
}
