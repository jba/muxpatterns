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
	// lit   string // literal or "/" for trailing slash
	// wild  bool
	// multi bool // true only for leaves
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
	segs := p.toSegments()
	root.addSegments(segs, p)
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

func (n *node) match(path string) *Pattern {
	// // Multi matches whatever's left in path, even empty.
	// if n.multi {
	// 	return n.pat
	// }
	// If path is empty, then return the node's pattern, which
	// may be nil.
	if path == "" {
		return n.pat
	}
	seg, rest := nextSegment(path)
	if c := n.children[seg]; c != nil {
		// TODO: backtracking.
		return c.match(rest)
	}
	// Match single wildcard.
	if c := n.children[""]; c != nil {
		return c.match(rest)
	}
	// Match multi wildcard.
	if c := n.children["*"]; c != nil {
		return c.match(rest)
	}
	return nil
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
