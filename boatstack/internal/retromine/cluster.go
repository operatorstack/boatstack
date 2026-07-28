package retromine

import (
	"regexp"
	"sort"
	"strings"
)

// Recurrence detection: normalize each operator instruction, reduce it to
// token 3-shingles, and greedily cluster by Jaccard similarity in a fixed
// order. A cluster is a recurrence candidate only when the same instruction
// shape appears at least minOccurrences times across at least minSessions
// distinct sessions — repetition inside one conversation is conversation,
// not steady-state error. Everything here is deterministic: inputs are
// sorted before clustering, so identical inputs in any order produce
// identical clusters.
// control-law: retro-derivation-is-offline-and-deterministic
const (
	// jaccardThreshold is the shingle-set similarity at which two
	// instructions count as the same instruction shape.
	jaccardThreshold = 0.6
	// minInstructionTokens filters acknowledgements and one-word replies
	// ("g", "ok", "yes please") out of the instruction pool.
	minInstructionTokens = 4
	// minOccurrences and minSessions define recurrence.
	minOccurrences = 3
	minSessions    = 2
	// exemplarCap bounds the quoted exemplar so a report never embeds a wall
	// of transcript text.
	exemplarCap = 240
)

// Cluster is one recurring instruction shape with its evidence.
type Cluster struct {
	Exemplar    string     `json:"exemplar"`
	Normalized  string     `json:"normalized"`
	Occurrences int        `json:"occurrences"`
	Sessions    []string   `json:"sessions"`
	Evidence    []EventRef `json:"evidence"`
}

var (
	fencedCodePattern = regexp.MustCompile("(?s)```.*?```")
	inlineCodeSpaces  = regexp.MustCompile("\\s+")
	nonWordPattern    = regexp.MustCompile(`[^a-z0-9 ]+`)
)

// normalizeInstruction reduces an operator message to its comparable shape:
// fenced code stripped (pasted logs are payload, not instruction), lowered,
// punctuation removed, whitespace collapsed.
func normalizeInstruction(text string) string {
	text = fencedCodePattern.ReplaceAllString(text, " ")
	text = strings.ToLower(text)
	text = nonWordPattern.ReplaceAllString(text, " ")
	return strings.TrimSpace(inlineCodeSpaces.ReplaceAllString(text, " "))
}

// shingles returns the token 3-shingle set; short instructions fall back to
// one whole-text shingle so they remain comparable.
func shingles(normalized string) map[string]bool {
	tokens := strings.Fields(normalized)
	set := map[string]bool{}
	if len(tokens) < 3 {
		if len(tokens) > 0 {
			set[strings.Join(tokens, " ")] = true
		}
		return set
	}
	for i := 0; i+3 <= len(tokens); i++ {
		set[strings.Join(tokens[i:i+3], " ")] = true
	}
	return set
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for key := range a {
		if b[key] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

type candidate struct {
	ref        EventRef
	text       string
	normalized string
	shingleSet map[string]bool
}

// DetectRecurrence finds the recurring operator instruction shapes across the
// given events. Only operator events participate; ordering of the input does
// not matter.
func DetectRecurrence(events []Event) []Cluster {
	candidates := []candidate{}
	// The per-session Index is intrinsic to each event (parser-assigned from
	// transcript order), so the caller's concatenation order is irrelevant.
	for _, event := range events {
		if event.Role != RoleOperator {
			continue
		}
		normalized := normalizeInstruction(event.Text)
		if len(strings.Fields(normalized)) < minInstructionTokens {
			continue
		}
		candidates = append(candidates, candidate{
			ref:        EventRef{SessionID: event.SessionID, Index: event.Index},
			text:       strings.TrimSpace(event.Text),
			normalized: normalized,
			shingleSet: shingles(normalized),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ref.SessionID != candidates[j].ref.SessionID {
			return candidates[i].ref.SessionID < candidates[j].ref.SessionID
		}
		return candidates[i].ref.Index < candidates[j].ref.Index
	})

	type bucket struct {
		representative candidate
		members        []candidate
	}
	buckets := []*bucket{}
	for _, c := range candidates {
		placed := false
		for _, b := range buckets {
			if jaccard(c.shingleSet, b.representative.shingleSet) >= jaccardThreshold {
				b.members = append(b.members, c)
				placed = true
				break
			}
		}
		if !placed {
			buckets = append(buckets, &bucket{representative: c, members: []candidate{c}})
		}
	}

	clusters := []Cluster{}
	for _, b := range buckets {
		sessions := map[string]bool{}
		refs := make([]EventRef, 0, len(b.members))
		for _, member := range b.members {
			sessions[member.ref.SessionID] = true
			refs = append(refs, member.ref)
		}
		if len(b.members) < minOccurrences || len(sessions) < minSessions {
			continue
		}
		names := make([]string, 0, len(sessions))
		for session := range sessions {
			names = append(names, session)
		}
		sort.Strings(names)
		exemplar := b.representative.text
		if len(exemplar) > exemplarCap {
			exemplar = exemplar[:exemplarCap] + "…"
		}
		clusters = append(clusters, Cluster{
			Exemplar:    exemplar,
			Normalized:  b.representative.normalized,
			Occurrences: len(b.members),
			Sessions:    names,
			Evidence:    refs,
		})
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Occurrences != clusters[j].Occurrences {
			return clusters[i].Occurrences > clusters[j].Occurrences
		}
		return clusters[i].Normalized < clusters[j].Normalized
	})
	return clusters
}
