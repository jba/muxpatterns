// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO:
// - check host validity to catch missing initial slash; e.g. "{x}/a".

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

// A segment is a pattern piece that matches one or more path elements.
// If wild is false, it matches a sub-path, including slashes.
// If wild is true and multi is false, it matches a single path element.
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
	p.host = rest[:i]
	rest = rest[i:]
	for len(rest) > 0 {
		// Invariant: rest[0] == '/'
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			break
		}
		if i > 0 && rest[i-1] != '/' {
			return nil, errors.New("bad wildcard segment (starts after '/')")
		}
		p.segments = append(p.segments, segment{s: rest[:i]})
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
		p.segments = append(p.segments, segment{wild: true, s: name, multi: multi})
	}
	if len(rest) > 0 {
		p.segments = append(p.segments, segment{s: rest})
		if rest[len(rest)-1] == '/' {
			p.segments = append(p.segments, segment{wild: true, multi: true})
		}
	}
	for _, s := range p.segments {
		if !s.wild {
			if strings.Contains(s.s, "//") {
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
	if len(p.segments) == 0 {
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
	for _, seg := range p.segments {
		if seg.multi {
			if seg.s != "" {
				matches = append(matches, rest)
			}
			rest = ""
		} else if seg.wild {
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
			rest, found = strings.CutPrefix(rest, seg.s)
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

// MoreSpecificThan reports whether p1 is more specific than p2, as defined
// by these rules:
//
//  1. Patterns with a host win over patterns without a host.
//  2. Patterns with a method win over patterns without a method.
//  3. Prefer patterns whose path component is more specific.
func (p1 *Pattern) MoreSpecificThan(p2 *Pattern) bool {
	// 1. Patterns with a host win over patterns without a host.
	if (p1.host == "") != (p2.host == "") {
		return p1.host != ""
	}
	// 2. Patterns with a method win over patterns without a method.
	if (p1.method == "") != (p2.method == "") {
		return p1.method != ""
	}
	// 3. More specific paths.
	return p1.comparePaths(p2) == moreSpecific
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
	return p1.comparePaths(p2) == overlaps
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
