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
		if ps.wild {
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

// A PatternSet is a set of non-conflicting patterns.
type PatternSet struct {
	mu       sync.Mutex
	patterns []*Pattern
}

func (s *PatternSet) Register(p *Pattern) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.patterns {
		if p.conflictsWith(q) {
			return fmt.Errorf("conflicting patterns %s and %s", q, p)
		}
	}
	s.patterns = append(s.patterns, p)
	return nil
}

func (s *PatternSet) MatchRequest(req *http.Request) (*Pattern, map[string]any, error) {
	return s.Match(req.Method, req.URL.Host, req.URL.Path)
}

func (s *PatternSet) Match(method, host, path string) (*Pattern, map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// var (
	// 	best    *Pattern
	// 	matches []string
	// )
	return nil, nil, nil
	// for _, p := range s.patterns {
	// 	if ok, ms := p.Match(method, host, path); ok {
	// 		if p.BetterMatchThan(best) {
	// 			best = p
	// 			matches = ms
	// 		}
	// 	}
	// }
	// if best == nil {
	// 	return nil, nil, nil
	// }
	// bindings, err := best.bind(matches)
	// return best, bindings, err
}

// 	if !strings.HasPrefix(path, "/") {
// 		panic("non-absolute path")
// 	}
// 	var ms []string
// 	for _, seg := range segs {
// 		if !seg.wild {
// 			if !strings.HasPrefix(path, seg.s) {
// 				return false, nil
// 			}
// 			path = path[len(seg.s):]
// 		} else if seg.multi {
// 			// Match the rest of path.
// 			if seg.s != "" {
// 				// Remove the initial slash, because it's unexpected.
// 				// That means there's no way to tell if "/a/{...}"
// 				// matched "/a" or "/a/".
// 				ms = append(ms, strings.TrimPrefix(path, "/"))
// 			}
// 			return true, ms
// 		} else {
// 			i := strings.IndexByte(path, '/')
// 			if i < 0 {
// 				i = len(path)
// 			}
// 			if seg.s != "" {
// 				ms = append(ms, path[:i])
// 			}
// 			path = path[i:]
// 		}
// 	}
// 	return true, ms
// }

// bind returns a map from wildcard names to matched, decoded values.
// matches is a list of matched substrings in the order that non-empty wildcards
// appear in the Pattern.
// func (p *Pattern) bind(matches []string) (map[string]any, error) {
// 	bindings := map[string]any{}
// 	i := 0
// 	for _, seg := range p.path {
// 		if seg.wild() && seg.s != "" {
// 			decode, ok := types.Load(seg.typ)
// 			if !ok {
// 				// Should never happen, because we found the type during parsing and
// 				// we never remove anything from types.
// 				return nil, fmt.Errorf("internal error: decoder for type %q not found", seg.typ)
// 			}
// 			val, err := decode.(decoder)(matches[i])
// 			if err != nil {
// 				return nil, err
// 			}
// 			i++
// 			bindings[seg.s] = val
// 		}
// 	}
// 	return bindings, nil
// }

// func (p1 *Pattern) BetterMatchThan(p2 *Pattern) bool {
// 	if p2 == nil {
// 		return true
// 	}
// 	if p1 == nil {
// 		return false
// 	}
// 	// 1. Patterns with a host win over patterns without a host.
// 	if (p1.host == "") != (p2.host == "") {
// 		return p1.host != ""
// 	}
// 	// 2. Patterns with a method win over patterns without a method.
// 	if (p1.method == "") != (p2.method == "") {
// 		return p1.method != ""
// 	}
// 	// 3. Patterns with a longer literal (non-wildcard) prefix win over patterns with shorter ones.
// 	return p1.literalPrefixLen() > p2.literalPrefixLen()
// }

// func (p *Pattern) literalPrefixLen() int {
// 	// Every path starts with a literal, unless it consists solely of a multi.
// 	if p.path[0].wild() {
// 		// multi, count 1 for the initial '/'
// 		return 1
// 	}
// 	n := len(p.path[0].s)
// 	if len(p.path) == 2 && p.path[1].multi {
// 		// Add 1 for '/'
// 		n++
// 	}
// 	return n
// }

// TODO: do these conflict?
//		/a/b/
//		/a/b/{$}
// Yes, because they have the same literal prefix length and overlap on path "/a/b/".

func (p1 *Pattern) conflictsWith(p2 *Pattern) bool {
	if p1.host != p2.host {
		// Either one host is empty and the other isn't, in which case the
		// one with the host is better by rule 1, or neither host is empty
		// and they differ, so they won't match the same paths.
		return false
	}
	if p1.method != p2.method {
		return false // By rule 2.
	}
	return false
	// if p1.literalPrefixLen() != p2.literalPrefixLen() {
	// 	return false // By rule 3.
	// }
	// return segsOverlap(p1.path, p2.path) != ""
}

// // segsOverlap returns a string matched by both segment lists, or "" if there is none.
// func segsOverlap(segs1, segs2 []segment) string {
// 	ov := strsOverlap(segsToString(segs1), segsToString(segs2))
// 	if ov == "." {
// 		return "/"
// 	}
// 	return strings.TrimSuffix(ov, ".")
// }

// func segsToString(segs []segment) string {
// 	var b strings.Builder
// 	for _, s := range segs {
// 		if !s.wild() {
// 			b.WriteString(s.s)
// 		} else if s.multi {
// 			b.WriteByte('.')
// 		} else {
// 			b.WriteByte('*')
// 		}
// 	}
// 	return b.String()
// }

// s1 and s2 are from segsToString.
// TODO: unicode, not bytes?
// func strsOverlap(s1, s2 string) string {
// 	var ov string
// 	for {
// 		i := mismatch(s1, s2) // Index of first byte that differs.
// 		ov += s1[:i]          // Everything before that is part of the overlap.
// 		s1 = s1[i:]           // Remove the common prefix.
// 		s2 = s2[i:]
// 		if s1 == "" && s2 == "" {
// 			return ov
// 		}
// 		// Multi matches the rest.
// 		if s1 == "." {
// 			return ov + s2
// 		}
// 		if s2 == "." {
// 			return ov + s1
// 		}
// 		if s1 == "" || s2 == "" {
// 			// Remainder cannot be matched.
// 			return ""
// 		}
// 		if s1[0] != '*' && s2[0] != '*' {
// 			// Two literals, which must be different
// 			// or they would have been in the common prefix.
// 			return ""
// 		}
// 		// Exactly one wildcard. Make s1 have it.
// 		if s2[0] == '*' {
// 			s1, s2 = s2, s1
// 		}
// 		// s1's wildcard matches s2's first path element.
// 		i = strings.IndexByte(s2, '/')
// 		if i < 0 {
// 			i = len(s2)
// 		}
// 		ov += s2[:i]
// 		s1 = s1[1:]
// 		s2 = s2[i:]
// 	}
// }

// index of first byte that differs
func mismatch(s1, s2 string) int {
	if len(s1) > len(s2) {
		s1, s2 = s2, s1
	}
	for i := 0; i < len(s1); i++ {
		if s1[i] != s2[i] {
			return i
		}
	}
	return len(s1)
}
