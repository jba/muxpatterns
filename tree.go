// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"golang.org/x/exp/maps"
)

type node struct {
	// special children keys:
	//     "/"	trailing slash
	//	   ""   single wildcard
	//	   "*"  multi wildcard
	children map[string]*node // interior node
	pat      *Pattern         // leaf
}

type segment struct {
	s     string // literal or "/"
	wild  bool
	multi bool
}

func (p *Pattern) toSegments() []segment {
	var segs []segment
	for _, e := range p.elements {
		if e.multi {
			segs = append(segs, segment{wild: true, multi: true})
		} else if e.wild {
			segs = append(segs, segment{wild: true})
		} else {
			parts := strings.Split(e.s, "/")
			if parts[0] == "" {
				parts = parts[1:]
			}
			if parts[len(parts)-1] == "" {
				parts = parts[:len(parts)-1]
			}
			for _, a := range parts {
				segs = append(segs, segment{s: a})
			}
		}
	}
	last := p.elements[len(p.elements)-1]
	if strings.HasSuffix(last.s, "/") {
		segs = append(segs, segment{s: "/"})
	}
	return segs
}

// returns segment, "/" for trailing slash, or "" for done.
// path should start with a "/"
func nextSegment(path string) (seg, rest string) {
	if path == "/" {
		return "/", ""
	}
	path = path[1:] // drop initial slash
	i := strings.IndexByte(path, '/')
	if i < 0 {
		return path, ""
	}
	return path[:i], path[i:]
}

func (root *node) addPattern(p *Pattern) {
	// First level of tree is host.
	n := root.addChild(p.host)
	// Second level of tree is method.
	n = n.addChild(p.method)
	// Remaining levels are path.
	n.addSegments(p.toSegments(), p)
}

func (n *node) addSegments(segs []segment, p *Pattern) {
	if len(segs) == 0 {
		if n.pat != nil {
			panic("n.pat != nil")
		}
		n.pat = p
		return
	}
	seg := segs[0]
	if seg.multi {
		if len(segs) != 1 {
			panic("multi wildcard not last")
		}
		if n.children["*"] != nil {
			panic("dup multi wildcards")
		}
		c := n.addChild("*")
		c.pat = p
	} else if seg.wild {
		n.addChild("").addSegments(segs[1:], p)
	} else {
		n.addChild(seg.s).addSegments(segs[1:], p)
	}
}

func (n *node) addChild(key string) *node {
	if n.children == nil {
		n.children = map[string]*node{}
	}
	if c := n.children[key]; c != nil {
		return c
	}
	c := &node{}
	n.children[key] = c
	return c
}

func (root *node) match(method, host, path string) (*Pattern, []string) {
	if c := root.children[host]; c != nil && host != "" {
		if p, m := c.matchMethodAndPath(method, path); p != nil {
			return p, m
		}
	}
	if c := root.children[""]; c != nil {
		return c.matchMethodAndPath(method, path)
	}
	return nil, nil
}

func (n *node) matchMethodAndPath(method, path string) (*Pattern, []string) {
	if c := n.children[method]; c != nil && method != "" {
		if p, m := c.matchPath(path, nil); p != nil {
			return p, m
		}
	}
	if c := n.children[""]; c != nil {
		return c.matchPath(path, nil)
	}
	return nil, nil
}

func (n *node) matchPath(path string, matches []string) (*Pattern, []string) {
	// If path is empty, then return the node's pattern, which
	// may be nil.
	if path == "" {
		return n.pat, matches
	}
	seg, rest := nextSegment(path)
	if c := n.children[seg]; c != nil {
		if p, m := c.matchPath(rest, matches); p != nil {
			return p, m
		}
	}
	// Match single wildcard.
	if c := n.children[""]; c != nil {
		if p, m := c.matchPath(rest, append(matches, seg)); p != nil {
			return p, m
		}
	}
	// Match multi wildcard to the rest of the pattern.
	if c := n.children["*"]; c != nil {
		return c.pat, append(matches, path[1:]) // remove initial slash
	}
	return nil, nil
}

func (n *node) print(w io.Writer, level int) {
	indent := strings.Repeat("    ", level)
	if n.pat != nil {
		fmt.Fprintf(w, "%s%q\n", indent, n.pat)
	} else {
		fmt.Fprintf(w, "%snil\n", indent)
	}
	keys := maps.Keys(n.children)
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "%s%q:\n", indent, k)
		n.children[k].print(w, level+1)
	}
}
