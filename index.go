// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import "math"

// An index optimizes conflict detection by indexing
// patterns.
type index struct {
	segments map[indexKey][]*Pattern
	multis   []*Pattern
}

type indexKey struct {
	pos int    // 0-based segment position
	s   string // literal, or empty for wildcard
}

func newIndex() *index {
	return &index{segments: map[indexKey][]*Pattern{}}
}

func (idx *index) addPattern(pat *Pattern) {
	if pat.lastSegment().multi {
		idx.multis = append(idx.multis, pat)
	} else {
		for pos, seg := range pat.segments {
			key := indexKey{pos: pos, s: ""}
			if !seg.wild {
				key.s = seg.s
			}
			idx.segments[key] = append(idx.segments[key], pat)
		}
	}
}

// possiblyConflictingPatterns calls f on all patterns that might conflict with pat.
func (idx *index) possiblyConflictingPatterns(pat *Pattern, f func(*Pattern) error) (err error) {
	// Terminology:
	//   dollar pattern: one ending in "{$}"
	//   multi pattern: one ending in a trailing slash or "{x...}" wildcard
	//   ordinary pattern: neither of the above

	apply := func(pats []*Pattern) {
		if err != nil {
			return
		}
		for _, p := range pats {
			err = f(p)
			if err != nil {
				break
			}
		}
	}

	switch {
	case pat.lastSegment().s == "/":
		// All paths that a dollar pattern matches end in a slash; no paths that an ordinary
		// pattern matches do. So only other dollar or multi patterns can conflict with a dollar pattern.
		// Furthermore, conflicting dollar patterns must have the {$} in the same position.
		apply(idx.segments[indexKey{s: "/", pos: len(pat.segments) - 1}])
		apply(idx.multis)
	default:
		// For ordinary patterns, the only conflicts can be with patterns that
		// have the same literal or a wildcard at some literal position,
		// or with a multi.
		// Find the position with the fewest patterns.
		var lmin, wmin []*Pattern
		min := math.MaxInt
		hasLit := false
		for i, seg := range pat.segments {
			if seg.multi {
				break
			}
			if !seg.wild {
				hasLit = true
				lpats := idx.segments[indexKey{s: seg.s, pos: i}]
				wpats := idx.segments[indexKey{s: "", pos: i}]
				sum := len(lpats) + len(wpats)
				if sum < min {
					lmin = lpats
					wmin = wpats
					min = sum
				}
			}
		}
		if !hasLit {
			// This pattern is all wildcards.
			// It can only conflict with a multi, or an equivalent pattern.
			apply(idx.segments[indexKey{s: "", pos: len(pat.segments) - 1}])
		} else {
			apply(lmin)
			apply(wmin)
		}
		apply(idx.multis)
		// A multi pattern can also conflict with a dollar pattern of the same
		// number of segments or more.
		// If the multi has a literal, we picked up these dollar patterns above.
		// If it doesn't, then it's more general than any of those dollar patterns.
		// So there is nothing extra to do.
	}
	return err
}
