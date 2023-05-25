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
	"sync"
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
	// Paths ending in "{$}" are represented with an element ending in '/'.
	// This makes most algorithms simpler.
	elements []element
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
	for _, s := range p.elements {
		if !s.wild {
			b.WriteString(s.s)
		} else {
			b.WriteByte('{')
			b.WriteString(s.s)
			if s.multi {
				b.WriteString("...")
			}
			b.WriteByte('}')
		}
	}
	return b.String()
}

// An element is a pattern piece that matches one or more path segments.
// If wild is false, it matches a literal sub-path, including slashes.
// If wild is true and multi is false, it matches a single path segment.
// If both wild and multi are true, it matches all remaining path elements.
type element struct {
	s     string // literal string or wildcard name
	wild  bool   // a wildcard
	multi bool   // wildcard ends in "..."
}

func (e element) String() string {
	if !e.wild {
		return e.s
	}
	dots := ""
	if e.multi {
		dots = "..."
	}
	return "{" + e.s + dots + "}"
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
	for len(rest) > 0 {
		// Invariant: rest[0] == '/'
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			break
		}
		if i > 0 && rest[i-1] != '/' {
			return nil, errors.New("bad wildcard segment (starts after '/')")
		}
		p.elements = append(p.elements, element{s: rest[:i]})
		rest = rest[i+1:]
		j := strings.IndexByte(rest, '}')
		if j < 0 {
			return nil, errors.New("bad wildcard segment: missing '}'")
		}
		if j == 0 {
			return nil, errors.New("empty wildcard")
		}
		name := rest[:j]
		rest = rest[j+1:]
		if len(rest) > 0 && rest[0] != '/' {
			return nil, errors.New("bad wildcard segment (ends before '/')")
		}
		if name == "$" {
			if len(rest) != 0 {
				return nil, errors.New("{$} not at end")
			}
			return p, nil
		}
		var multi bool
		if strings.HasSuffix(name, "...") {
			multi = true
			name = name[:len(name)-3]
			if len(rest) != 0 {
				return nil, errors.New("{...} wildcard not at end")
			}
		}
		if !isValidWildcardName(name) {
			return nil, fmt.Errorf("bad wildcard name %q", name)
		}
		p.elements = append(p.elements, element{wild: true, s: name, multi: multi})
	}
	if len(rest) > 0 {
		p.elements = append(p.elements, element{s: rest})
		if rest[len(rest)-1] == '/' {
			p.elements = append(p.elements, element{wild: true, multi: true})
		}
	}
	for _, e := range p.elements {
		if !e.wild {
			if strings.Contains(e.s, "//") {
				return nil, errors.New("empty path segment")
			}
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

// Match reports whether p matches the method, host and path.
// The method and host may be empty.
// The path must start with a '/'
// If the first return value is true, the second is the list of wildcard matches,
// in the order the wildcards occur in p.
//
// A wildcard other than "$" that does not end in "..." matches a non-empty
// path segment. So "/{x}" matches "/a" but not "/".
//
// A wildcard that ends in "..." can match the empty string, or a sequence of path segments.
// So "/{x...}" matches the paths "/", "/a", "/a/" and "/a/b". In each case, the string
// associated with "x" is the path with the initial slash removed.
//
// The wildcard "{$}" matches the empty string, but only after a final slash.
func (p *Pattern) Match(method, host, path string) (bool, []string) {
	if len(path) == 0 || path[0] != '/' {
		panic("path should start with '/'")
	}
	if len(p.elements) == 0 {
		panic("pattern has no segments")
	}

	if p.method != "" && method != p.method {
		return false, nil
	}
	if p.host != "" && host != p.host {
		return false, nil
	}
	rest := path
	var matches []string
	for _, el := range p.elements {
		if el.multi {
			if el.s != "" {
				matches = append(matches, rest)
			}
			rest = ""
		} else if el.wild {
			i := strings.IndexByte(rest, '/')
			if i < 0 {
				i = len(rest)
			}
			if i == 0 {
				// Ordinary wildcard matching empty string.
				return false, nil
			}
			matches = append(matches, rest[:i])
			rest = rest[i:]
		} else {
			var found bool
			rest, found = strings.CutPrefix(rest, el.s)
			if !found {
				return false, nil
			}
		}
	}
	if len(rest) > 0 {
		return false, nil
	}
	return true, matches
}

// HigherPrecedence reports whether p1 has higher precedence than p2.
// If p1 and p2 both match a request, then p1 will be chosen.
//
// Precedence is defined by these rules:
//
//  1. Patterns with a host win over patterns without a host.
//  2. Patterns with a method win over patterns without a method.
//  3. Patterns whose path is more specific win. One path pattern is more
//     specific than another if the second matches all the paths of the
//     first and more.
func (p1 *Pattern) HigherPrecedence(p2 *Pattern) bool {
	// 1. Patterns with a host win over patterns without a host.
	if (p1.host == "") != (p2.host == "") {
		return p1.host != ""
	}
	// 2. Patterns with a method win over patterns without a method.
	if (p1.method == "") != (p2.method == "") {
		return p1.method != ""
	}
	// 3. More specific paths win.
	return p1.comparePaths(p2) == moreSpecific
}

// ConflictsWith reports whether p1 conflicts with p2, that is, whether
// there is a request that both match but where neither is higher precedence
// than the other.
func (p1 *Pattern) ConflictsWith(p2 *Pattern) bool {
	if p1.host != p2.host {
		// Either one host is empty and the other isn't, in which case the
		// one with the host is more specific by rule 1, or neither host is empty
		// and they differ, so they won't match the same paths.
		return false
	}
	if p1.method != p2.method {
		// Same reasoning as above, with rule 2.
		return false
	}
	return p1.comparePaths(p2) == overlaps
}

const (
	moreSpecific = "moreSpecific"
	moreGeneral  = "moreGeneral"
	overlaps     = "overlaps"
	disjoint     = "disjoint"
)

// comparePaths classifies the paths of the patterns into one of four
// groups:
//
//	moreGeneral: p1 matches all the paths of p2 and more
//	moreSpecific: p2 matches all the paths of p1 and more
//	overlaps: there is a path that both match, but neither is more specific
//	disjoint: there is no path that both match
func (p1 *Pattern) comparePaths(p2 *Pattern) string {
	// Copy the segment slices to make the algorithm simpler.
	// TODO: avoid the copy; simplify the entire algorithm.
	segs1 := slices.Clone(p1.elements)
	segs2 := slices.Clone(p2.elements)

	// Track whether a single (non-multi) wildcard in p1 matched
	// a literal in p2, and vice versa.
	// We care about these because if a wildcard matches a literal, then the
	// pattern with the wildcard can't be more specific than the one with the
	// literal.
	wild1MatchedLit2 := false
	wild2MatchedLit1 := false
	for len(segs1) > 0 && len(segs2) > 0 {
		s1 := segs1[0]
		s2 := segs2[0]
		if s1.multi && s2.multi {
			segs1 = segs1[1:]
			segs2 = segs2[1:]
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
			// p2 matches the rest of p1.
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

// A PatternSet is a set of non-conflicting patterns.
// The zero value is an empty PatternSet, ready for use.
type PatternSet struct {
	mu       sync.Mutex
	patterns []*Pattern
}

// Register adds a Pattern to the set. If returns an error
// if the pattern conflicts with an existing pattern in the set.
func (s *PatternSet) Register(p *Pattern) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.patterns {
		if p.ConflictsWith(q) {
			return fmt.Errorf("new pattern %s conflicts with existing pattern %s", p, q)
		}
	}
	s.patterns = append(s.patterns, p)
	return nil
}

// MatchRequest calls Match with the request's method, host and path.
func (s *PatternSet) MatchRequest(req *http.Request) (*Pattern, map[string]string, error) {
	return s.Match(req.Method, req.URL.Host, req.URL.Path)
}

// Match matches the method, host and path against the patterns in the PatternSet.
// It returns the highest-precedence matching pattern and a map from wildcard
// names to matching path segments.
// Match returns (nil, nil, nil) if there is no matching pattern.
func (s *PatternSet) Match(method, host, path string) (*Pattern, map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var (
		best    *Pattern
		matches []string
	)
	for _, p := range s.patterns {
		if ok, ms := p.Match(method, host, path); ok {
			if best == nil || p.HigherPrecedence(best) {
				best = p
				matches = ms
			}
		}
	}
	if best == nil {
		return nil, nil, nil
	}
	bindings, err := best.bind(matches)
	return best, bindings, err
}

// bind returns a map from wildcard names to matched, decoded values.
// matches is a list of matched substrings in the order that non-empty wildcards
// appear in the Pattern.
func (p *Pattern) bind(matches []string) (map[string]string, error) {
	bindings := map[string]string{}
	i := 0
	for _, seg := range p.elements {
		if seg.wild && seg.s != "" {
			bindings[seg.s] = matches[i]
			i++
		}
	}
	return bindings, nil
}
