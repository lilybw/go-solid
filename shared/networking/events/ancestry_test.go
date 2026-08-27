package events

import "testing"

func Test_ancestryOfTContainsT(t *testing.T) {
	for _, eV := range EVENTS.Values {
		selfAndParents, ok := ancestry[eV]
		if !ok || len(selfAndParents) == 0 {
			t.Fatalf("No ancestry for %v", eV)
		}
		if selfAndParents[0] != eV {
			t.Fatalf("Expected ancestry of %v to begin with it self", eV)
		}
	}
}

func Test_ancestryContainsOnlyUnique(t *testing.T) {
	for k, v := range ancestry {
		set := make(map[EventType]EventType)
		for _, t := range v {
			set[t] = t
		}
		keys := make([]EventType, 0, len(set))
		for et := range set {
			keys = append(keys, et)
		}
		if len(keys) != len(v) {
			t.Fatalf("Ancestry of %v contained duplicate entries. Found: %v", k, v)
		}
	}
}
