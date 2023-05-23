// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO:
// check for overlap because of same method or host and overlapping paths???

package muxpatterns

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

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

// A Pattern is something that can be matched against a an HTTP request.
type Pattern struct {
	method   string
	host     string
	segments []segment
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
		b.WriteByte('/')
		b.WriteString(s.String())
	}
	return b.String()
}

// A segment is a pattern piece that matches one or more path elements.
// If multi is false, it matches a single path element.
// Otherwise, it matches all remaining path elements.
type segment struct {
	s     string // literal string or wildcard name
	wild  bool   // a wildcard
	multi bool   // wildcard ends in "..."
}

func (s segment) String() string {
	if !s.wild {
		return s.s
	}
	dots := ""
	if s.multi {
		dots = "..."
	}
	return "{" + s.s + dots + "}"
}

func (s segment) isDollar() bool {
	return s.wild && s.s == "$"
}

// TODO(jba): fail if anything comes after {$}

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
// There must be a single space after METHOD if it is present.
// Wildcard names must be valid Go identifiers.
// PATH may end with a '/', unless the final segment is "{$}" or a  "..." wildcard.
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
	if i >= 0 {
		p.host = rest[:i]
		rest = rest[i+1:]
	}
	trailingSlash := true
	for len(rest) > 0 {
		// Invariant: previous character of rest was '/'.
		var first string
		first, rest, trailingSlash = strings.Cut(rest, "/")
		if len(first) == 0 {
			return nil, errors.New("empty path segment")
		}
		var seg segment
		if first[0] != '{' {
			// literal
			if strings.IndexByte(first, '{') > 0 {
				return nil, fmt.Errorf("bad wildcard segment %q", first)
			}
			seg = segment{wild: false, s: first}
		} else {
			// wildcard
			if first[len(first)-1] != '}' {
				return nil, fmt.Errorf("bad wildcard segment %q", first)
			}
			name := first[1 : len(first)-1]
			multi := false
			if strings.HasSuffix(name, "...") {
				multi = true
				name = name[:len(name)-3]
			}
			if !isValidWildcardName(name) {
				return nil, fmt.Errorf("bad wildcard name %q", name)
			}
			if name == "$" && multi {
				return nil, fmt.Errorf("bad wildcard: %q", first)
			}
			if (name == "$" || multi) && (trailingSlash || len(rest) > 0) {
				return nil, errors.New("'$' or '...' must be at end")
			}
			seg = segment{wild: true, s: name, multi: multi}
		}
		p.segments = append(p.segments, seg)
	}
	if trailingSlash {
		// Represent a trailing slash as a final multi segment with no name.
		p.segments = append(p.segments, segment{wild: true, multi: true})
	}
	return p, nil
}

func isValidPathLiteral(s string) bool {
	return utf8.ValidString(s)
}

func isValidWildcardName(s string) bool {
	if s == "" {
		return false
	}
	if s == "$" {
		return true
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
	if len(p.segments) == 0 {
		panic("pattern has no segments")
	}

	if p.method != "" && method != p.method {
		return false, nil
	}
	if p.host != "" && host != p.host {
		return false, nil
	}
	rest := path[1:]
	i := 0
	var matches []string
	trailingSlash := true
	for len(rest) > 0 {
		// Invariant: previous char of rest was '/'
		if i >= len(p.segments) {
			// TODO: could figure this out earlier.
			return false, nil
		}
		ps := p.segments[i]
		if ps.multi {
			// Multi wildcard matches the rest of the path.
			if ps.s == "" {
				return true, matches
			}
			return true, append(matches, rest)
		}
		// Get next path segment.
		var rs string
		rs, rest, trailingSlash = strings.Cut(rest, "/")
		if ps.wild && ps.s != "$" {
			matches = append(matches, rs)
			i++
			continue
		}
		// Literals must match exactly.
		if rs != ps.s {
			return false, nil
		}
		i++
	}
	if i == len(p.segments) {
		if trailingSlash {
			return false, nil
		}
		return true, matches
	}
	// {$} and {...} match a trailing slash.
	if ps := p.segments[i]; ps.wild && trailingSlash {
		if ps.multi {
			if ps.s == "" {
				return true, matches
			}
			return true, append(matches, "")
		}
		if ps.s == "$" {
			return true, matches
		}
	}
	// If there are pattern segments left, the pattern did not match.
	return false, nil
}

// MoreSpecificThan reports whether p1 is more specific than p2, as defined
// by these rules:
//
//  1. Patterns with a host win over patterns without a host.
//  2. Patterns with a method win over patterns without a method.
//  3. Patterns with a longer literal (non-wildcard) prefix win over patterns
//     with shorter ones.
//  4. Patterns that don't end in a multi win over patterns that do.
//
// MoreSpecificThan is not a order on patterns:
// it may be that neither of two patterns is more specific than the other.
func (p1 *Pattern) MoreSpecificThan(p2 *Pattern) bool {
	// 1. Patterns with a host win over patterns without a host.
	if (p1.host == "") != (p2.host == "") {
		return p1.host != ""
	}
	// 2. Patterns with a method win over patterns without a method.
	if (p1.method == "") != (p2.method == "") {
		return p1.method != ""
	}
	// 3. Patterns with a longer literal (non-wildcard) prefix win over patterns
	// with shorter ones.
	if l1, l2 := p1.literalPrefixLen(), p2.literalPrefixLen(); l1 != l2 {
		return l1 > l2
	}
	//  4. Patterns that don't end in a multi win over patterns that do.
	if m1, m2 := p1.lastSeg().multi, p2.lastSeg().multi; m1 != m2 {
		return m2
	}
	return false
}

func (p *Pattern) literalPrefixLen() int {
	n := 0
	for _, s := range p.segments {
		if s.wild {
			return n + 1 // for final slash
		}
		n += 1 + len(s.s) // +1 for preceding slash
	}
	return n
}

func (p *Pattern) lastSeg() segment {
	return p.segments[len(p.segments)-1]
}

// ConflictsWith reports whether p1 conflicts with p2, that is, whether
// there is a request that both match but where p1 is not more specific than p2.
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
	if p1.MoreSpecificThan(p2) || p2.MoreSpecificThan(p1) {
		return false
	}

	// Make p1 the one with fewer segments.
	if len(p1.segments) > len(p2.segments) {
		p1, p2 = p2, p1
	}

	for i, s1 := range p1.segments {
		s2 := p2.segments[i]
		// If corresponding literal segments differ, there is no overlap.
		if !s1.wild && !s2.wild && s1.s != s2.s {
			return false
		}
		// The {$} matches only paths ending in '/', and literals and ordinary wildcards do not match '/'.
	}
	if len(p1.segments) == len(p2.segments) {
		// Both patterns have the same number of segments.
		// We haven't ruled out overlap, meaning that each pair of corresponding
		// segments is either the same literal, or at least one is a wildcard.
		// The only case where they don't overlap is where one final segment is {$} and the other is not a multi.
		s1 := p1.lastSeg()
		s2 := p2.lastSeg()
		if s1.isDollar() && s2.isDollar() {
			return true
		}
		if (s1.isDollar() && !s2.multi) || (s2.isDollar() && !s1.multi) {
			return false
		}
		return true
	}
	// p2 has more segments than p1.
	// If the last segment of p1 is a multi, it matches the rest of p2, so there is
	// overlap.
	// Otherwise, there isn't: the last segment of p1 is either {$}, in which case it
	// doesn't match the corresponding segment of p2 (which is not {$} or {...}), or
	// it is a literal or ordinary wildcard, which means there is at least one more segment
	// of p2 that it doesn't match.
	return p1.segments[len(p1.segments)-1].multi
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

// MatchRequest matches an http.Request against the set of patterns, returning
// the matching pattern and a map from wildcard to matching path segment.
func (s *PatternSet) MatchRequest(req *http.Request) (*Pattern, map[string]any, error) {
	return s.Match(req.Method, req.URL.Host, req.URL.Path)
}

func (s *PatternSet) Match(method, host, path string) (*Pattern, map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var (
		best    *Pattern
		matches []string
	)
	for _, p := range s.patterns {
		if ok, ms := p.Match(method, host, path); ok {
			if best == nil || p.MoreSpecificThan(best) {
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
func (p *Pattern) bind(matches []string) (map[string]any, error) {
	bindings := map[string]any{}
	i := 0
	for _, seg := range p.segments {
		if seg.wild && seg.s != "" && seg.s != "$" {
			bindings[seg.s] = matches[i]
			i++
		}
	}
	return bindings, nil
}
