package collections

import (
	"iter"
	"maps"
	"slices"
)

type SetMap[K, V comparable] map[K]map[V]struct{}

func (s SetMap[K, V]) Add(key K, member V) {
	set, ok := s[key]
	if !ok {
		set = map[V]struct{}{}
		s[key] = set
	}
	set[member] = struct{}{}
}

func (s SetMap[K, V]) Remove(key K, member V) {
	set, ok := s[key]
	if !ok {
		return
	}
	delete(set, member)
	if len(set) == 0 {
		delete(s, key)
	}
}
func (s SetMap[K, V]) Drop(key K)                { delete(s, key) }
func (s SetMap[K, V]) Members(key K) iter.Seq[V] { return maps.Keys(s[key]) }
func (s SetMap[K, V]) MembersOf(key K) []V {
	if len(s[key]) == 0 {
		return nil
	}
	return slices.Collect(s.Members(key))
}

func (s SetMap[K, V]) Keys() iter.Seq[K] { return maps.Keys(s) }
func (s SetMap[K, V]) KeysWhere(pred func(K) bool) []K {
	var out []K
	for key := range s {
		if pred(key) {
			out = append(out, key)
		}
	}
	return out
}
