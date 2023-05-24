// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"strings"

	"golang.org/x/exp/slices"
)

const (
	moreSpecific = "moreSpecific"
	moreGeneral  = "moreGeneral"
	overlaps     = "overlaps"
	disjoint     = "disjoint"
)

func (p1 *Pattern) comparePaths(p2 *Pattern) string {
	// Copy the segment slices to make the algorithm simpler.
	// In a production implementation we'd do something different.
	segs1 := slices.Clone(p1.segments)
	segs2 := slices.Clone(p2.segments)

	wild1MatchedLit2 := false
	wild2MatchedLit1 := false
	for len(segs1) > 0 && len(segs2) > 0 {
		s1 := segs1[0]
		s2 := segs2[0]
		if s1.multi && s2.multi {
			if wild1MatchedLit2 && !wild2MatchedLit1 {
				return moreGeneral
			}
			if wild2MatchedLit1 && !wild1MatchedLit2 {
				return moreSpecific
			}
			return overlaps
		}
		if s1.multi {
			// p1 matches the rest of p2.
			if !wild2MatchedLit1 {
				return moreGeneral
			}
			return overlaps
		}
		if s2.multi {
			if !wild1MatchedLit2 {
				return moreSpecific
			}
			return overlaps
		}
		if s1.wild && s2.wild {
			// These ordinary wildcards match each other.
		} else if s1.wild {
			// p1's ordinary wildcard matches the first segment
			// of p2's literal, or the rest of it.
			_, s2.s = splitSegment(s2.s)
			wild1MatchedLit2 = true
		} else if s2.wild {
			_, s1.s = splitSegment(s1.s)
			wild2MatchedLit1 = true
		} else {
			// Two literals.
			// Consume the common prefix, or fail.
			i := len(s1.s)
			if len(s1.s) > len(s2.s) {
				i = len(s2.s)
			}
			if s1.s[:i] != s2.s[:i] {
				return disjoint
			}
			s1.s = s1.s[i:]
			s2.s = s2.s[i:]
		}
		// If this segment is done, advance to the next.
		if s1.wild || s1.s == "" {
			segs1 = segs1[1:]
		} else {
			segs1[0].s = s1.s // Assign back into slice; s1 is a copy.
		}
		if s2.wild || s2.s == "" {
			segs2 = segs2[1:]
		} else {
			segs2[0].s = s2.s
		}
	}
	if len(segs1) == 0 && len(segs2) == 0 {
		// The patterns matched completely.
		if wild1MatchedLit2 && !wild2MatchedLit1 {
			return moreGeneral
		}
		if wild2MatchedLit1 && !wild1MatchedLit2 {
			return moreSpecific
		}
		return overlaps
	}
	if len(segs1) == 0 {
		// p2 has at least one more segment.
		// p1 did not end in a multi.
		// That means p2's remaining segments must match nothing.
		// Which means to match, there must be only one segment, a multi.
		if !segs2[0].multi {
			return disjoint
		}
		if !wild1MatchedLit2 {
			return moreSpecific
		}
		return overlaps
	}
	// len(segs2) == 0
	if !segs1[0].multi {
		return disjoint
	}
	if !wild2MatchedLit1 {
		return moreGeneral
	}
	return overlaps
}

// #################################################################
// TODO: coverage on the above; simplify it.

func splitSegment(path string) (string, string) {
	i := strings.IndexByte(path, '/')
	if i < 0 {
		return path, ""
	}
	return path[:i], path[i:]
}
