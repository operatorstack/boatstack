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
	output.WriteString("<!-- Generated from catalog.Default by surfaces.RenderCatalogMarkdown. Do not edit. -->\n")
	output.WriteString("# Boatstack V2 executable transition catalog\n\n")
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
	output.WriteString("| Transition | Class | Source phases | Target phases | Authority | Parameters | Owned resources | Recovery |\n")
	output.WriteString("|---|---|---|---|---|---|---|---|\n")
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
		fmt.Fprintf(&output, "| `%s` | %s | %s | %s | %s | %s | %s | `%s` |\n",
			transition.ID, transition.Class, joinPhases(transition.SourcePhases), joinPhases(transition.TargetPhases),
			authority, markdownList(parameters), markdownList(transition.OwnedResources), recovery)
	}
	output.WriteString("\n`*` marks a required parameter. OR authority is shown with `/`; mandatory authority clauses are shown with `AND`. Source and target facet predicates remain in the canonical JSON returned by `boatstack catalog --format json`.\n")
	return output.String()
}

// RenderCatalogMermaid generates one inventory node per runtime transition,
// grouped by event class. Phase sets are labels on the exact transition node,
// avoiding a second hand-maintained graph.
func RenderCatalogMermaid(transitions []catalog.Transition) string {
	ordered := append([]catalog.Transition(nil), transitions...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Class != ordered[j].Class {
			return ordered[i].Class < ordered[j].Class
		}
		return ordered[i].ID < ordered[j].ID
	})
	classes := []catalog.EventClass{catalog.EventAuthority, catalog.EventOwnedLocal, catalog.EventOwnedExternal, catalog.EventRecovery, catalog.EventObservedExternal}
	var output strings.Builder
	output.WriteString("%% Generated from catalog.Default by surfaces.RenderCatalogMermaid. Do not edit.\n")
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
