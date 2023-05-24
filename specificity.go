// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"strings"

	"golang.org/x/exp/slices"
)

func (p *Pattern) pathMatchString() string {
	var b strings.Builder
	for _, s := range p.segments {
		b.WriteByte('/')
		if !s.wild {
			b.WriteString(s.s)
		} else if s.s == "$" {
		} else if !s.multi {
			b.WriteByte('{')
			b.WriteString(s.s)
			b.WriteByte('}')
		} else {
		}
	}
	return b.String()
}

/*
func (p1 *Pattern) cmpSpecific(p2 *Pattern) string {
	fmt.Printf("-- cmpSpecific %s vs. %s\n", p1, p2)
	ov := p1.overlap(p2)
	if ov == "" {
		return none
	}

	s1 := p1.pathMatchString()
	s2 := p2.pathMatchString()
	// fmt.Printf("%s => %s\n", p1, s1)
	// fmt.Printf("%s => %s\n", p2, s2)

	ok1, _ := p1.Match("", "", s2)
	// if ok1 {
	// 	fmt.Printf("    %s matches %s\n", p1, s2)
	// }
	ok2, _ := p2.Match("", "", s1)
	// if ok2 {
	// 	fmt.Printf("    %s matches %s\n", p2, s1)
	// }

	if ok1 && !ok2 {
		return right
	}
	if !ok1 && ok2 {
		return left
	}
	return overlaps
}
*/

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

func splitSegment(path string) (string, string) {
	i := strings.IndexByte(path, '/')
	if i < 0 {
		return path, ""
	}
	return path[:i], path[i:]
}

/*


	// Let p1 be the shorter one.
	if len(p1.segments) > len(p2.segments) {
		p1, p2 = p2, p1
	}
	for i, s1 := range p1.segments {
		b.WriteByte('/')
		s2 := p2.segments[i]
		// Check different literals.
		if !s1.wild && !s2.wild && s1.s != s2.s {
			return ""
		}
		switch {
		case !s1.wild && !s2.wild:
			b.WriteString(s1.s)
		case s1.isDollar():
			if s2.isDollar() || s2.multi {
				return b.String()
			}
			// If s2 is an ordinary wildcard or a literal, then s1's {$}
			// won't match.
			return ""
		case s1.multi:
			if s2.isDollar() || s2.multi {
				return b.String()
			}
			// e.g. /a/{...} vs. /a/b/{...}
			b.WriteString(s2.s)
			for _, s2s := range p2.segments[i+1:] {
				b.WriteByte('/')
				if s2s.isDollar() || s2s.multi {
					break
				}
				b.WriteString(s2s.s)
			}
			return b.String()

		case s1.wild: // ordinary wildcard
			if !s2.wild {
				b.WriteString(s2.s)
			} else if s2.isDollar() { // /{x} vs. /{$}: no overlap
				return ""
			} else { // s2 is a wildcard, maybe a multi
				b.WriteString(s1.s)
			}

		default: // s1 is a literal, s2 is wild
			if s2.isDollar() {
				return ""
			}
			b.WriteString(s1.s)
		}
	}
	// If there are more p2 segments, they can't match.
	if len(p2.segments) > len(p1.segments) {
		return ""
	}
	return b.String()
}
*/
// 	l1 := p1.lastSeg()
// 	l2 := p2.lastSeg()
// 	if !l1.multi && !l2.multi {
// 		if l1.isDollar() != l2.isDollar() {
// 			return ""
// 		}
// 		if len(p1.segments) != len(p2.segments) {
// 			return ""
// 		}
// 		for i, s1 := range p1.segments {
// 			b.WriteByte('/')
// 			s2 := p2.segments[i]
// 			var s string
// 			switch {
// 			case s1.wild && s2.wild:
// 				s = "_"
// 			case s1.wild:
// 				s = s2.s
// 			case s2.wild:
// 				s = s1.s
// 			default:
// 				if s1.s != s2.s {
// 					return ""
// 				}
// 				s = s1.s
// 			}
// 			b.WriteString(s)
// 		}
// 		if l1.isDollar() {
// 			b.WriteByte('/')
// 		}
// 		return b.String()
// 	}
// 	return "UNIMP"
// }
