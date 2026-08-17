package hostprojection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ID identifies a repository host projection. Projection selection controls
// generated files only; it is not runtime-host authority.
type ID string

const (
	Codex  ID = "codex"
	Claude ID = "claude"
	Cursor ID = "cursor"
	Gemini ID = "gemini"
)

const SelectionSchemaVersion = 1

var canonical = []ID{Claude, Codex, Cursor, Gemini}
var generatedSlug = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
var lowercaseSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func CanonicalIDs() []ID { return append([]ID(nil), canonical...) }

func CanonicalStrings() []string {
	result := make([]string, len(canonical))
	for index, id := range canonical {
		result[index] = string(id)
	}
	return result
}

func ValidSHA256(value string) bool { return lowercaseSHA256.MatchString(value) }

// Parse validates an explicit projection selection against the enabled
// runtime hosts and returns it in canonical order. A nil selection represents
// a missing or null required JSON field; an explicit empty slice is valid.
func Parse(values, hosts []string) ([]ID, error) {
	if values == nil {
		return nil, fmt.Errorf("PROJECT_PROJECTIONS_REQUIRED: Boatstack project configuration requires explicit projections")
	}
	result, err := ParseIDs(values)
	if err != nil {
		return nil, err
	}
	enabled := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		enabled[host] = true
	}
	for _, id := range result {
		if !enabled[string(id)] {
			return nil, fmt.Errorf("PROJECT_PROJECTION_HOST_DISABLED: projection %q requires the matching runtime host", id)
		}
	}
	return result, nil
}

// ParseIDs validates an explicit projection set without assigning runtime-host
// authority. It is used by artifacts that bind selection but do not own host
// admission policy.
func ParseIDs(values []string) ([]ID, error) {
	if values == nil {
		return nil, fmt.Errorf("PROJECT_PROJECTIONS_REQUIRED: Boatstack project configuration requires explicit projections")
	}
	allowed := make(map[ID]bool, len(canonical))
	for _, id := range canonical {
		allowed[id] = true
	}
	seen := make(map[ID]bool, len(values))
	result := make([]ID, 0, len(values))
	for _, value := range values {
		id := ID(value)
		if !allowed[id] || seen[id] {
			return nil, fmt.Errorf("PROJECT_PROJECTION_INVALID: unsupported or duplicated projection %q", value)
		}
		seen[id] = true
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func Strings(values []ID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func SelectionFingerprint(values []ID) (string, error) {
	canonicalValues, err := ParseIDs(Strings(values))
	if err != nil {
		return "", err
	}
	payload := struct {
		SchemaVersion int      `json:"schema_version"`
		Projections   []string `json:"projections"`
	}{SchemaVersion: SelectionSchemaVersion, Projections: Strings(canonicalValues)}
	if payload.Projections == nil {
		payload.Projections = []string{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func FlowPaths(id ID, slug string) ([]string, error) {
	if !validSlug(slug) || slug == "boatstack-update" {
		return nil, fmt.Errorf("FLOW_PROJECTION_PATH_INVALID: invalid or reserved Flow projection slug %q", slug)
	}
	switch id {
	case Codex:
		root := filepath.ToSlash(filepath.Join(".agents", "skills", slug))
		return []string{root + "/.gitattributes", root + "/SKILL.md", root + "/agents/openai.yaml"}, nil
	case Claude:
		root := filepath.ToSlash(filepath.Join(".claude", "skills", slug))
		return []string{root + "/.gitattributes", root + "/SKILL.md"}, nil
	case Cursor:
		return []string{filepath.ToSlash(filepath.Join(".cursor", "commands", slug+".md"))}, nil
	case Gemini:
		return []string{filepath.ToSlash(filepath.Join(".gemini", "skills", slug, "SKILL.md"))}, nil
	default:
		return nil, fmt.Errorf("FLOW_PROJECTION_PATH_INVALID: unsupported projection %q", id)
	}
}

// SharedCheckoutPath returns checkout metadata shared by every Flow projected
// to a host. These paths are reference-counted separately from slug-scoped
// outputs because several Flow ownership records may bind the same file.
func SharedCheckoutPath(id ID) (string, []byte, bool) {
	switch id {
	case Cursor:
		return ".cursor/commands/.gitattributes", []byte(".gitattributes -text\n*.md -text\n"), true
	case Gemini:
		return ".gemini/skills/.gitattributes", []byte(".gitattributes -text\n** -text\n"), true
	default:
		return "", nil, false
	}
}

func IsSharedCheckoutPath(path string) bool {
	for _, id := range canonical {
		candidate, _, ok := SharedCheckoutPath(id)
		if ok && path == candidate {
			return true
		}
	}
	return false
}

// MaintenancePaths returns the complete canonical repository projection for
// the Kernel-owned update driver. Shared host-level attributes are part of the
// maintenance manifest so they have one serialized owner across all Flows.
func MaintenancePaths(id ID) ([]string, error) {
	switch id {
	case Codex:
		root := ".agents/skills/boatstack-update"
		return []string{root + "/.gitattributes", root + "/SKILL.md", root + "/agents/openai.yaml"}, nil
	case Claude:
		root := ".claude/skills/boatstack-update"
		return []string{root + "/.gitattributes", root + "/SKILL.md"}, nil
	case Cursor:
		return []string{".cursor/commands/.gitattributes", ".cursor/commands/boatstack-update.md"}, nil
	case Gemini:
		return []string{".gemini/skills/.gitattributes", ".gemini/skills/boatstack-update/SKILL.md"}, nil
	default:
		return nil, fmt.Errorf("HOST_PROJECTION_PATH_INVALID: unsupported projection %q", id)
	}
}

func ValidMaintenancePath(path string) bool {
	if filepath.ToSlash(filepath.Clean(path)) != path {
		return false
	}
	for _, id := range canonical {
		paths, _ := MaintenancePaths(id)
		for _, candidate := range paths {
			if path == candidate {
				return true
			}
		}
	}
	return false
}

func ValidFlowPath(value string) bool {
	if IsSharedCheckoutPath(value) {
		return true
	}
	if !safeRelative(value) {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) == 4 && (parts[0] == ".agents" || parts[0] == ".claude") && parts[1] == "skills" && validSlug(parts[2]) && parts[2] != "boatstack-update" && parts[3] == ".gitattributes" {
		return true
	}
	if len(parts) == 4 && parts[0] == ".agents" && parts[1] == "skills" && validSlug(parts[2]) && parts[2] != "boatstack-update" && parts[3] == "SKILL.md" {
		return true
	}
	if len(parts) == 5 && parts[0] == ".agents" && parts[1] == "skills" && validSlug(parts[2]) && parts[2] != "boatstack-update" && parts[3] == "agents" && parts[4] == "openai.yaml" {
		return true
	}
	if len(parts) == 4 && parts[0] == ".claude" && parts[1] == "skills" && validSlug(parts[2]) && parts[2] != "boatstack-update" && parts[3] == "SKILL.md" {
		return true
	}
	if len(parts) == 3 && parts[0] == ".cursor" && parts[1] == "commands" && strings.HasSuffix(parts[2], ".md") {
		return validSlug(strings.TrimSuffix(parts[2], ".md")) && strings.TrimSuffix(parts[2], ".md") != "boatstack-update"
	}
	return len(parts) == 4 && parts[0] == ".gemini" && parts[1] == "skills" && validSlug(parts[2]) && parts[2] != "boatstack-update" && parts[3] == "SKILL.md"
}

func validSlug(value string) bool { return generatedSlug.MatchString(value) }

func safeRelative(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && filepath.ToSlash(clean) == value
}
