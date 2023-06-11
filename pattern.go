// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package muxpatterns is a sample implementation of enhanced
// http.ServeMux routing patterns.
// See https://github.com/golang/go/discussions/60227.
//
// The API in this package is for experimentation only.
// It is likely that none of it will be in the proposal.
package muxpatterns

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"golang.org/x/exp/slices"
)

// Valid HTTP methods.
var methods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
	http.MethodTrace,
}

// A Pattern is something that can be matched against an HTTP request.
type Pattern struct {
	method string
	host   string
	// The representation of a path differs from the surface syntax.
	// Paths ending in '/' are represented with an anonymous "..." wildcard.
	// Paths ending in "{$}" are represented with the literal segment "/".
	// This makes most algorithms simpler.
	segments []segment
}

// A segment is a pattern piece that matches one or more path segments, or
// a trailing slash.
// If wild is false, it matches a literal segment, or, if s == "/", a trailing slash.
// If wild is true and multi is false, it matches a single path segment.
// If both wild and multi are true, it matches all remaining path segments.
type segment struct {
	s     string // literal or wildcard name or "/" for "/{$}".
	wild  bool
	multi bool // "..." wildcard
}

func (p *Pattern) Method() string { return p.method }

func (p *Pattern) String() string {
	var b strings.Builder
	if p.method != "" {
		b.WriteString(p.method)
		b.WriteByte(' ')
	}
	if p.host != "" {
		b.WriteString(p.host)
	}
	for _, s := range p.segments {
		b.WriteString(s.String())
	}
	return b.String()
}

func (s segment) String() string {
	switch {
	case s.multi && s.s == "": // Trailing slash.
		return "/"
	case s.multi:
		return fmt.Sprintf("/{%s...}", s.s)
	case s.wild:
		return fmt.Sprintf("/{%s}", s.s)
	case s.s == "/":
		return "/{$}"
	default: // Literal.
		return "/" + s.s
	}
}

// Parse parses a string into a Pattern.
// The string's syntax is
//
//	[METHOD] [HOST]/[PATH]
//
// where:
//   - METHOD is the uppercase name of an HTTP method
//   - HOST is a hostname
//   - PATH consists of slash-separated segments, where each segment is either
//     a literal or a wildcard of the form "{name}", "{name...}", or "{$}".
//
// METHOD, HOST and PATH are all optional; that is, the string can be "/".
// If METHOD is present, it must be followed by a single space.
// Wildcard names must be valid Go identifiers.
// The "{$}" and "{name...}" wildcard must occur at the end of PATH.
// PATH may end with a '/'.
// Wildcard names in a path must be distinct.
func Parse(s string) (*Pattern, error) {
	if len(s) == 0 {
		return nil, errors.New("empty pattern")
	}
	method, rest, found := strings.Cut(s, " ")
	if !found {
		rest = method
		method = ""
	}

	p := &Pattern{method: method}
	if method != "" && !slices.Contains(methods, method) {
		return nil, fmt.Errorf("bad method %q; need one of %v", method, methods)
	}

	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return nil, errors.New("host/path missing /")
	}
	p.host = rest[:i]
	rest = rest[i:]
	if strings.IndexByte(p.host, '{') >= 0 {
		return nil, errors.New("host contains '{' (missing initial '/'?")
	}
	// At this point, rest is the path.

	// An unclean path with a method that is not CONNECT can never match,
	// because paths are cleaned before matching.
	if method != "" && method != "CONNECT" && rest != cleanPath(rest) {
		return nil, errors.New("non-CONNECT pattern with unclean path can never match")
	}

	seenNames := map[string]bool{}
	for len(rest) > 0 {
		// Invariant: rest[0] == '/'.
		rest = rest[1:]
		if len(rest) == 0 {
			// Trailing slash.
			p.segments = append(p.segments, segment{wild: true, multi: true})
			break
		}
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			i = len(rest)
		}
		var seg string
		seg, rest = rest[:i], rest[i:]
		if i := strings.IndexByte(seg, '{'); i < 0 {
			// Literal.
			p.segments = append(p.segments, segment{s: seg})
		} else {
			// Wildcard.
			if i != 0 {
				return nil, errors.New("bad wildcard segment (must start with '{')")
			}
			if seg[len(seg)-1] != '}' {
				return nil, errors.New("bad wildcard segment (must end with '}')")
			}
			name := seg[1 : len(seg)-1]
			if name == "$" {
				if len(rest) != 0 {
					return nil, errors.New("{$} not at end")
				}
				p.segments = append(p.segments, segment{s: "/"})
				break
			}
			var multi bool
			if strings.HasSuffix(name, "...") {
				multi = true
				name = name[:len(name)-3]
				if len(rest) != 0 {
					return nil, errors.New("{...} wildcard not at end")
				}
			}
			if name == "" {
				return nil, errors.New("empty wildcard")
			}
			if !isValidWildcardName(name) {
				return nil, fmt.Errorf("bad wildcard name %q", name)
			}
			if seenNames[name] {
				return nil, fmt.Errorf("duplicate wildcard name %q", name)
			}
			seenNames[name] = true
			p.segments = append(p.segments, segment{s: name, wild: true, multi: multi})
		}
	}
	return p, nil
}

func isValidWildcardName(s string) bool {
	if s == "" {
		return false
	}
	// Valid Go identifier.
	for i, c := range s {
		if !unicode.IsLetter(c) && c != '_' && (i == 0 || !unicode.IsDigit(c)) {
			return false
		}
	}
	return true
}

// HigherPrecedence reports whether p1 has higher precedence than p2.
// If p1 and p2 both match a request, then p1 will be chosen.
//
// Precedence is defined by these rules:
//
//  1. Patterns with a host win over patterns without a host.
//  2. Patterns whose method and path is more specific win. One pattern is more
//     specific than another if the second matches all the (method, path) pairs
//     of the first and more.
func (p1 *Pattern) HigherPrecedence(p2 *Pattern) bool {
	// 1. Patterns with a host win over patterns without a host.
	if (p1.host == "") != (p2.host == "") {
		return p1.host != ""
	}
	// 2. More specific (method, path)s win.
	return p1.comparePathsAndMethods(p2) == moreSpecific
}

// ConflictsWith reports whether p1 conflicts with p2, that is, whether
// there is a request that both match but where neither is higher precedence
// than the other.
func (p1 *Pattern) ConflictsWith(p2 *Pattern) bool {
	if p1.host != p2.host {
		// Either one host is empty and the other isn't, in which case the
		// one with the host wins by rule 1, or neither host is empty
		// and they differ, so they won't match the same paths.
		return false
	}
	rel := p1.comparePathsAndMethods(p2)
	return rel == equivalent || rel == overlaps
}

// relationship is a relationship between two patterns.
type relationship string

const (
	moreSpecific relationship = "moreSpecific"
	moreGeneral  relationship = "moreGeneral"
	overlaps     relationship = "overlaps"
	equivalent   relationship = "equivalent"
	disjoint     relationship = "disjoint"
)

func (p1 *Pattern) comparePathsAndMethods(p2 *Pattern) relationship {
	mr := p1.compareMethods(p2)
	// Optimization: avoid a call to comparePaths.
	if mr == disjoint {
		return disjoint
	}
	pr := p1.comparePaths(p2)
	return combineRelationships(mr, pr)
}

func combineRelationships(methodRel, pathRel relationship) relationship {
	switch {
	case methodRel == equivalent:
		return pathRel
	case methodRel == moreGeneral:
		switch pathRel {
		case equivalent:
			return moreGeneral
		case moreSpecific:
			return overlaps
		default:
			return pathRel
		}
	case methodRel == moreSpecific:
		// The dual of the above.
		switch pathRel {
		case equivalent:
			return moreSpecific
		case moreGeneral:
			return overlaps
		default:
			return pathRel
		}
	default:
		// Different non-empty methods.
		return disjoint
	}
}

func (p1 *Pattern) compareMethods(p2 *Pattern) relationship {
	if p1.method == p2.method {
		return equivalent
	}
	if p1.method == "" {
		return moreGeneral
	}
	if p2.method == "" {
		return moreSpecific
	}
	return disjoint
}

// comparePaths determines the relationship between two patterns,
// as far as paths are concerned.
//
//	equivalent: p1 and p2 match the same paths
//	moreGeneral: p1 matches all the paths of p2 and more
//	moreSpecific: p2 matches all the paths of p1 and more
//	overlaps: there is a path that both match, but neither is more specific
//	disjoint: there is no path that both match
func (p1 *Pattern) comparePaths(p2 *Pattern) relationship {
	// Track whether a single (non-multi) wildcard in p1 matched
	// a literal in p2, and vice versa.
	// We care about these because if a wildcard matches a literal, then the
	// pattern with the wildcard can't be more specific than the one with the
	// literal.
	wild1MatchedLit2 := false
	wild2MatchedLit1 := false
	var segs1, segs2 []segment
	for segs1, segs2 = p1.segments, p2.segments; len(segs1) > 0 && len(segs2) > 0; segs1, segs2 = segs1[1:], segs2[1:] {
		s1 := segs1[0]
		s2 := segs2[0]
		if s1.multi && s2.multi {
			// Two multis match each other.
			continue
		}
		if s1.multi {
			// p1 matches the rest of p2.
			// Does that mean it is more general than p2?
			if !wild2MatchedLit1 {
				// If p2 didn't have any wildcards that matched literals in p1,
				// then yes, p1 is more general.
				return moreGeneral
			}
			// Otherwise neither is more general than the other.
			return overlaps
		}
		if s2.multi {
			// p2 matches the rest of p1. The same logic as above applies.
			if !wild1MatchedLit2 {
				return moreSpecific
			}
			return overlaps
		}
		if s1.s == "/" && s2.s == "/" {
			// Both patterns end in "/{$}"; they match.
			continue
		}
		if s1.s == "/" || s2.s == "/" {
			// One pattern ends in "/{$}", and the other doesn't, nor is the other's
			// corresponding segment a multi. So they are disjoint.
			return disjoint
		}
		if s1.wild && s2.wild {
			// These single-segment wildcards match each other.
		} else if s1.wild {
			// p1's single wildcard matches the corresponding segment of p2.
			wild1MatchedLit2 = true
		} else if s2.wild {
			// p2's single wildcard matches the corresponding segment of p1.
			wild2MatchedLit1 = true
		} else {
			// Two literal segments.
			if s1.s != s2.s {
				return disjoint
			}
		}
	}
	// We've reached the end of the corresponding segments of the patterns.
	if len(segs1) == 0 && len(segs2) == 0 {
		// The patterns matched completely.
		switch {
		case wild1MatchedLit2 && !wild2MatchedLit1:
			return moreGeneral
		case wild2MatchedLit1 && !wild1MatchedLit2:
			return moreSpecific
		case !wild1MatchedLit2 && !wild2MatchedLit1:
			return equivalent
		default:
			return overlaps
		}
	}
	// One pattern has more segments than the other.
	// The only way they can fail to be disjoint is if one ends in a multi, but
	// we handled that case in the loop.
	return disjoint
}

// DescribeRelationship returns a string that describes how pat1 and pat2
// are related.
func DescribeRelationship(pat1, pat2 string) string {
	p1, err := Parse(pat1)
	if err != nil {
		panic(err)
	}
	p2, err := Parse(pat2)
	if err != nil {
		panic(err)
	}
	return describeRel(p1, p2)
}

func describeRel(p1, p2 *Pattern) string {
	if p1.host != p2.host {
		switch {
		case p1.host == "":
			return fmt.Sprintf("%s does not have a host, while %s does, so %[2]s takes precedence", p1, p2)
		case p2.host == "":
			return fmt.Sprintf("%s does not have a host, while %s does, so %[2]s takes precedence", p2, p1)
		default:
			return fmt.Sprintf("%s and %s have different hosts, so they have no requests in common", p1, p2)
		}
	}
	methodRel := p1.compareMethods(p2)
	pathRel := p1.comparePaths(p2)
	rel := combineRelationships(methodRel, pathRel)
	switch rel {
	case disjoint:
		return fmt.Sprintf("%s has no requests in common with %s.", p1, p2)
	case equivalent:
		return fmt.Sprintf("%s matches the same requests as %s.", p1, p2)
	case moreSpecific:
		return moreSpecificMessage(p1, p2, methodRel)
	case moreGeneral:
		if methodRel == moreGeneral {
			methodRel = moreSpecific
		}
		return moreSpecificMessage(p2, p1, methodRel)
	case overlaps:
		return fmt.Sprintf(`%[1]s and %[2]s both match some paths, like %[3]q.
But neither is more specific than the other.
%[1]s matches %[4]q, but %[2]s doesn't.
%[2]s matches %[5]q, but %[1]s doesn't.`,
			p1, p2, commonPath(p1, p2), differencePath(p1, p2), differencePath(p2, p1))
	default: // overlap
		panic(fmt.Sprintf("bad relationship %q", rel))
	}
}

func moreSpecificMessage(spec, gen *Pattern, methodRel relationship) string {
	// Either the method or path is more specific, or both.
	over := matchingPath(spec)
	if methodRel == moreSpecific {
		// spec.method is not empty, gen.method is.
		return fmt.Sprintf(`%q is more specific than %q.
Both match "%s %s".
Only %[2]s matches "%[5]s %s".`,
			spec, gen,
			spec.method, over,
			otherMethod(spec.method), over)
	}
	diff := differencePath(gen, spec)
	return fmt.Sprintf(`%s is more specific than %s.
Both match path %s.
Only %[2]s matches path %[4]q.`,
		spec, gen, over, diff)
}

func matchingPath(p *Pattern) string {
	var b strings.Builder
	writeMatchingPath(&b, p.segments)
	return b.String()
}

// writeMatchingPath writes to b a path that matches the segments.
func writeMatchingPath(b *strings.Builder, segs []segment) {
	for _, s := range segs {
		writeSegment(b, s)
	}
}

func writeSegment(b *strings.Builder, s segment) {
	b.WriteByte('/')
	if !s.multi && s.s != "/" {
		b.WriteString(s.s)
	}
}

// commonPath returns a path that both p1 and p2 match.
// It assumes there is such a path.
func commonPath(p1, p2 *Pattern) string {
	var b strings.Builder
	var segs1, segs2 []segment
	for segs1, segs2 = p1.segments, p2.segments; len(segs1) > 0 && len(segs2) > 0; segs1, segs2 = segs1[1:], segs2[1:] {
		if s1 := segs1[0]; s1.wild {
			writeSegment(&b, segs2[0])
		} else {
			writeSegment(&b, s1)
		}
	}
	if len(segs1) > 0 {
		writeMatchingPath(&b, segs1)
	} else if len(segs2) > 0 {
		writeMatchingPath(&b, segs2)
	}
	return b.String()
}

func otherMethod(method string) string {
	i := slices.Index(methods, method)
	if i < 0 {
		return "BADMETHOD"
	}
	return methods[(i+1)%len(methods)]
}

// differencePath returns a path that p1 matches and p2 doesn't.
// It assumes there is such a path.
func differencePath(p1, p2 *Pattern) string {
	b := new(strings.Builder)

	var segs1, segs2 []segment
	for segs1, segs2 = p1.segments, p2.segments; len(segs1) > 0 && len(segs2) > 0; segs1, segs2 = segs1[1:], segs2[1:] {
		s1 := segs1[0]
		s2 := segs2[0]
		if s1.multi && s2.multi {
			// From here the patterns match the same paths, so we must have found a difference earlier.
			b.WriteByte('/')
			return b.String()

		}
		if s1.multi && !s2.multi {
			// s1 ends in a "..." wildcard but s2 does not.
			// A trailing slash will distinguish them, unless s2 ends in "{$}",
			// in which case any segment will do; prefer the wildcard name if
			// it has one.
			b.WriteByte('/')
			if s2.s == "/" {
				if s1.s != "" {
					b.WriteString(s1.s)
				} else {
					b.WriteString("x")
				}
			}
			return b.String()
		}
		if !s1.multi && s2.multi {
			writeSegment(b, s1)
		} else if s1.wild && s2.wild {
			// Both patterns will match whatever we put here; use
			// the first wildcard name.
			writeSegment(b, s1)
		} else if s1.wild && !s2.wild {
			// s1 is a wildcard, s2 is a literal.
			// Any segment other than s2.s will work.
			// Prefer the wildcard name, but if it's the same as the literal,
			// tweak the literal.
			if s1.s != s2.s {
				writeSegment(b, s1)
			} else {
				b.WriteByte('/')
				b.WriteString(s2.s + "x")
			}
		} else if !s1.wild && s2.wild {
			writeSegment(b, s1)
		} else {
			// Both are literals. A precondition of this function is that the
			// patterns overlap, so they must be the same literal. Use it.
			if s1.s != s2.s {
				fmt.Printf("%q, %q\n", s1.s, s2.s)
				panic("literals differ")
			}
			writeSegment(b, s1)
		}
	}
	if len(segs1) > 0 {
		// p1 is longer than p2, and p2 does not end in a multi.
		// Anything that matches the rest of p1 will do.
		writeMatchingPath(b, segs1)
	} else if len(segs2) > 0 {
		writeMatchingPath(b, segs2)
	}
	return b.String()
}
