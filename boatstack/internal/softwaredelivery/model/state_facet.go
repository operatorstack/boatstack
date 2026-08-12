package model

import (
	"fmt"
	"sort"
)

// StateFacet names one kernel-owned semantic domain in durable State. It is
// deliberately distinct from FacetName, which names resolver predicates.
type StateFacet string

const (
	StateFacetInstallation StateFacet = "installation"
	StateFacetProgram      StateFacet = "program"
	StateFacetControl      StateFacet = "control"
	StateFacetProduct      StateFacet = "product"
)

func (f StateFacet) Valid() bool {
	switch f {
	case StateFacetInstallation, StateFacetProgram, StateFacetControl, StateFacetProduct:
		return true
	default:
		return false
	}
}

func NormalizeStateFacets(name string, facets []StateFacet) ([]StateFacet, error) {
	result := append([]StateFacet(nil), facets...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	for index, facet := range result {
		if !facet.Valid() {
			return nil, fmt.Errorf("%s contains invalid state facet %q", name, facet)
		}
		if index > 0 && result[index-1] == facet {
			return nil, fmt.Errorf("%s duplicates state facet %q", name, facet)
		}
	}
	return result, nil
}

func UnionStateFacets(groups ...[]StateFacet) []StateFacet {
	set := map[StateFacet]bool{}
	for _, group := range groups {
		for _, facet := range group {
			if facet.Valid() {
				set[facet] = true
			}
		}
	}
	result := make([]StateFacet, 0, len(set))
	for facet := range set {
		result = append(result, facet)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
