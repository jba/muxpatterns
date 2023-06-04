// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"golang.org/x/exp/maps"
)

type node struct {
	// special children keys:
	//     "/"	trailing slash
	//	   ""   single wildcard
	//	   "*"  multi wildcard
	children   *hybrid // map[string]*node // interior node
	emptyChild *node   // child with key ""
	// leaf fields
	pattern  *Pattern
	handler  http.Handler
	location string // source location of registering call
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

func (root *node) addPattern(p *Pattern, h http.Handler, loc string) {
	// First level of tree is host.
	n := root.addChild(p.host)
	// Second level of tree is method.
	n = n.addChild(p.method)
	// Remaining levels are path.
	n.addSegments(p.segments, p, h, loc)
}

func (n *node) addSegments(segs []segment, p *Pattern, h http.Handler, loc string) {
	if len(segs) == 0 {
		n.set(p, h, loc)
		return
	}
	seg := segs[0]
	if seg.multi {
		if len(segs) != 1 {
			panic("multi wildcard not last")
		}
		if n.findChild("*") != nil {
			panic("dup multi wildcards")
		}
		c := n.addChild("*")
		c.set(p, h, loc)
	} else if seg.wild {
		n.addChild("").addSegments(segs[1:], p, h, loc)
	} else {
		n.addChild(seg.s).addSegments(segs[1:], p, h, loc)
	}
}

func (n *node) set(p *Pattern, h http.Handler, loc string) {
	if n.pattern != nil || n.handler != nil {
		panic("non-nil leaf fields")
	}
	n.pattern = p
	n.handler = h
	n.location = loc
}

func (n *node) addChild(key string) *node {
	if key == "" {
		if n.emptyChild == nil {
			n.emptyChild = &node{}
		}
		return n.emptyChild
	}
	if c := n.findChild(key); c != nil {
		return c
	}
	c := &node{}
	if n.children == nil {
		n.children = newHybrid(8)
	}
	n.children.add(key, c)
	return c
}

func (n *node) findChild(key string) *node {
	return n.children.get(key)
}

// TODO: version without matches for ServeMux.shouldRedirect.
func (root *node) match(method, host, path string) (*node, []string) {
	if host != "" {
		if c := root.findChild(host); c != nil {
			if p, m := c.matchMethodAndPath(method, path); p != nil {
				return p, m
			}
		}
	}
	if c := root.emptyChild; c != nil {
		return c.matchMethodAndPath(method, path)
	}
	return nil, nil
}

func (n *node) matchMethodAndPath(method, path string) (*node, []string) {
	if method == "" {
		panic("empty method")
	}
	if c := n.findChild(method); c != nil {
		if p, m := c.matchPath(path, nil); p != nil {
			return p, m
		}
	}
	if c := n.emptyChild; c != nil {
		return c.matchPath(path, nil)
	}
	return nil, nil
}

func (n *node) matchPath(path string, matches []string) (*node, []string) {
	// If path is empty, then return the node, whose pattern may be nil.
	if path == "" {
		if n.pattern == nil {
			return nil, nil
		}
		return n, matches
	}
	seg, rest := nextSegment(path)
	if c := n.findChild(seg); c != nil {
		if n, m := c.matchPath(rest, matches); n != nil {
			return n, m
		}
	}
	// Match single wildcard.
	if c := n.emptyChild; c != nil {
		if n, m := c.matchPath(rest, append(matches, seg)); n != nil {
			return n, m
		}
	}
	// Match multi wildcard to the rest of the pattern.
	if c := n.findChild("*"); c != nil {
		return c, append(matches, path[1:]) // remove initial slash
	}
	return nil, nil
}

func (n *node) patterns(f func(*Pattern, http.Handler, string) error) error {
	if n == nil {
		return nil
	}
	if n.pattern != nil {
		return f(n.pattern, n.handler, n.location)
	}
	if n.emptyChild != nil {
		if err := n.emptyChild.patterns(f); err != nil {
			return err
		}
	}
	return n.children.patterns(f)
}

// Modifies n; use for testing only.
func (n *node) print(w io.Writer, level int) {
	indent := strings.Repeat("    ", level)
	if n.pattern != nil {
		fmt.Fprintf(w, "%s%q\n", indent, n.pattern)
	} else {
		fmt.Fprintf(w, "%snil\n", indent)
	}
	if n.emptyChild != nil {
		fmt.Fprintf(w, "%s%q:\n", indent, "")
		n.emptyChild.print(w, level+1)
	}

	keys := n.children.keys()
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(w, "%s%q:\n", indent, k)
		n.children.get(k).print(w, level+1)
	}
}

type hybrid struct {
	maxSlice int
	s        []entry
	m        map[string]*node
}

type entry struct {
	key   string
	child *node
}

func newHybrid(ms int) *hybrid {
	return &hybrid{
		maxSlice: ms,
	}
}

func (h *hybrid) add(k string, v *node) {
	if h.m == nil && len(h.s) < h.maxSlice {
		h.s = append(h.s, entry{k, v})
	} else {
		if h.m == nil {
			h.m = map[string]*node{}
			for _, e := range h.s {
				h.m[e.key] = e.child
			}
			h.s = nil
		}
		h.m[k] = v
	}
}

func (h *hybrid) get(k string) *node {
	if h == nil {
		return nil
	}
	if h.m != nil {
		return h.m[k]
	}
	for _, e := range h.s {
		if e.key == k {
			return e.child
		}
	}
	return nil
}

func (h *hybrid) keys() []string {
	if h == nil {
		return nil
	}
	if h.m != nil {
		return maps.Keys(h.m)
	}
	var keys []string
	for _, e := range h.s {
		keys = append(keys, e.key)
	}
	return keys
}

func (h *hybrid) patterns(f func(*Pattern, http.Handler, string) error) error {
	if h == nil {
		return nil
	}
	if h.m != nil {
		for _, n := range h.m {
			if err := n.patterns(f); err != nil {
				return err
			}
		}
	} else {
		for _, e := range h.s {
			if err := e.child.patterns(f); err != nil {
				return err
			}
		}
	}
	return nil
}
