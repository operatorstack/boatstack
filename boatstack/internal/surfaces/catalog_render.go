package surfaces

import (
	"fmt"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

// RenderCatalogMarkdown projects the executable registry into a reviewable
// architecture artifact. It contains no independently maintained transitions.
func RenderCatalogMarkdown(transitions []catalog.Transition) string {
	ordered := append([]catalog.Transition(nil), transitions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	counts := map[catalog.EventClass]int{}
	for _, transition := range ordered {
		counts[transition.Class]++
	}
	var output strings.Builder
	output.WriteString("<!-- Generated from the compiled ControlProgram registry by surfaces.RenderCatalogMarkdown. Do not edit. -->\n")
	output.WriteString("# Boatstack compiled transition catalog\n\n")
	fmt.Fprintf(&output, "Registry size: **%d** transitions. Event classes: authority %d; owned-local %d; owned-external %d; recovery %d; observed-external %d.\n\n",
		len(ordered), counts[catalog.EventAuthority], counts[catalog.EventOwnedLocal], counts[catalog.EventOwnedExternal], counts[catalog.EventRecovery], counts[catalog.EventObservedExternal])
	output.WriteString("Controlling facets: `")
	facets := model.ControllingFacets()
	for index, facet := range facets {
		if index > 0 {
			output.WriteString("`, `")
		}
		output.WriteString(string(facet))
	}
	output.WriteString("`.\n\n")
	output.WriteString("| Transition | Origin | Owner | Selection | Class | Source phases | Target phases | Authority | Parameters | Owned resources | Verifier | Recovery | Cost |\n")
	output.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, transition := range ordered {
		authority := joinAuthorities(transition.Authority)
		if len(transition.AuthorityAll) > 0 {
			authority += " AND " + joinAuthorities(transition.AuthorityAll)
		}
		parameters := make([]string, 0, len(transition.Parameters))
		for _, parameter := range transition.Parameters {
			name := parameter.Name
			if parameter.Required {
				name += "*"
			}
			parameters = append(parameters, name)
		}
		recovery := string(transition.Interruption.Recovery)
		if recovery == "" {
			recovery = "-"
		}
		origin := fmt.Sprintf("%s:`%s@%s`<br/>`%s`", transition.Origin.Kind, transition.Origin.ID, transition.Origin.Version, transition.Origin.ManifestFingerprint)
		fmt.Fprintf(&output, "| `%s` | %s | `%s` | %s | %s | %s | %s | %s | %s | %s | `%s` | `%s` | `%s` |\n",
			transition.ID, origin, transition.Owner, transition.SelectionClass, transition.Class,
			joinPhases(transition.SourcePhases), joinPhases(transition.TargetPhases), authority,
			markdownList(parameters), markdownList(transition.OwnedResources), transition.Verifier, recovery, transition.CostClass)
	}
	output.WriteString("\n`*` marks a required parameter. OR authority is shown with `/`; mandatory authority clauses are shown with `AND`. Source and target facet predicates remain in the canonical JSON returned by `boatstack catalog --format json`.\n")
	return output.String()
}

// RenderCatalogMermaid generates one inventory node per runtime transition,
// grouped by event class. Phase sets are labels on the exact transition node,
// avoiding a second hand-maintained graph.
func RenderCatalogMermaid(transitions []catalog.Transition) string {
	return renderCatalogMermaid(transitions, "%% Generated from the compiled ControlProgram registry by surfaces.RenderCatalogMermaid. Do not edit.\n")
}

// RenderStandardFlowMermaid projects only the compiled primary-flow
// declarations. The owner filter is metadata from the same executable registry,
// not a second transition graph.
func RenderStandardFlowMermaid(transitions []catalog.Transition) string {
	flow := make([]catalog.Transition, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Origin.Kind == catalog.OriginPrimaryFlow {
			flow = append(flow, transition)
		}
	}
	return renderCatalogMermaid(flow, "%% Generated from compiled PrimaryFlow declarations by surfaces.RenderStandardFlowMermaid. Do not edit.\n")
}

func renderCatalogMermaid(transitions []catalog.Transition, header string) string {
	ordered := append([]catalog.Transition(nil), transitions...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Class != ordered[j].Class {
			return ordered[i].Class < ordered[j].Class
		}
		return ordered[i].ID < ordered[j].ID
	})
	classes := []catalog.EventClass{catalog.EventAuthority, catalog.EventOwnedLocal, catalog.EventOwnedExternal, catalog.EventRecovery, catalog.EventObservedExternal}
	var output strings.Builder
	output.WriteString(header)
	output.WriteString("flowchart TB\n")
	index := 0
	for _, class := range classes {
		fmt.Fprintf(&output, "  subgraph %s[\"%s\"]\n", strings.ReplaceAll(string(class), "-", "_"), class)
		for _, transition := range ordered {
			if transition.Class != class {
				continue
			}
			label := fmt.Sprintf("%s<br/>%s → %s", transition.ID, strings.Join(phaseStrings(transition.SourcePhases), " | "), strings.Join(phaseStrings(transition.TargetPhases), " | "))
			fmt.Fprintf(&output, "    t%02d[\"%s\"]\n", index, escapeMermaid(label))
			index++
		}
		output.WriteString("  end\n")
	}
	return output.String()
}

func joinAuthorities(values []catalog.AuthorityClass) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = string(value)
	}
	return strings.Join(parts, "/")
}

func phaseStrings(values []model.ProtocolPhase) []string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = string(value)
	}
	return parts
}

func joinPhases(values []model.ProtocolPhase) string {
	return strings.Join(phaseStrings(values), " / ")
}

func markdownList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return "`" + strings.Join(values, "`, `") + "`"
}

func escapeMermaid(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	return value
}
