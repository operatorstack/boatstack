package kernel

import "testing"

func TestRelationOwnsSelectionAndAuthority(t *testing.T) {
	candidates := []RelationCandidate{
		{ID: "low", Rank: 2, Priority: 1, Selectable: true},
		{ID: "top", Rank: 1, Priority: 1, Selectable: true, RequiredAll: []Capability{"execute"}},
	}
	without := Relate(RelationInput{Candidates: candidates})
	if without.Kind != Frontier || without.Transition != "top" {
		t.Fatalf("without authority = %#v", without)
	}
	with := Relate(RelationInput{Candidates: candidates, Available: []Capability{"execute"}})
	if with.Kind != Prescribed || with.Transition != "top" {
		t.Fatalf("with authority = %#v", with)
	}
}

func TestRelationTargetedAndUntargetedUseSameCandidates(t *testing.T) {
	candidates := []RelationCandidate{{ID: "advance", Priority: 1, Selectable: true}}
	untargeted := Relate(RelationInput{Candidates: candidates})
	targeted := Relate(RelationInput{Requested: "advance", Candidates: candidates})
	if untargeted.Kind != Prescribed || targeted.Kind != Prescribed || untargeted.Transition != targeted.Transition {
		t.Fatalf("untargeted/targeted = %#v/%#v", untargeted, targeted)
	}
	refused := Relate(RelationInput{Requested: "other", Candidates: candidates})
	if refused.Kind != Refused {
		t.Fatalf("inadmissible target = %#v", refused)
	}
}

func TestRelationReportsEqualPreferenceAndMarkedState(t *testing.T) {
	tied := Relate(RelationInput{Candidates: []RelationCandidate{
		{ID: "a", Priority: 1, Selectable: true},
		{ID: "b", Priority: 1, Selectable: true},
	}})
	if tied.Kind != Frontier || len(tied.Candidates) != 2 {
		t.Fatalf("tie = %#v", tied)
	}
	marked := Relate(RelationInput{Marked: true})
	if marked.Kind != Marked {
		t.Fatalf("marked = %#v", marked)
	}
}
