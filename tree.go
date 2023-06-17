// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements a decision tree for fast
// matching of requests to patterns.

package muxpatterns

import (
	"net/http"
	"strings"
)

// A node is a node in the decision tree.
// The same struct is used for leaf and interior nodes.
type node struct {
	// A leaf node holds a single pattern and the Handler it was registered
	// with.
	pattern *Pattern
	handler http.Handler

	// An interior node maps parts of the incoming request to child nodes.
	// special children keys:
	//     "/"	trailing slash
	//	   ""   single wildcard
	//	   "*"  multi wildcard
	children   mapping[string, *node]
	emptyChild *node // optimization: child with key ""
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

func (root *node) addPattern(p *Pattern, h http.Handler) {
	// First level of tree is host.
	n := root.addChild(p.host)
	// Second level of tree is method.
	n = n.addChild(p.method)
	// Remaining levels are path.
	n.addSegments(p.segments, p, h)
}

func (n *node) addSegments(segs []segment, p *Pattern, h http.Handler) {
	if len(segs) == 0 {
		n.set(p, h)
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
		c.set(p, h)
	} else if seg.wild {
		n.addChild("").addSegments(segs[1:], p, h)
	} else {
		n.addChild(seg.s).addSegments(segs[1:], p, h)
	}
}

func (n *node) set(p *Pattern, h http.Handler) {
	if n.pattern != nil || n.handler != nil {
		panic("non-nil leaf fields")
	}
	n.pattern = p
	n.handler = h
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
	n.children.add(key, c)
	return c
}

func (n *node) findChild(key string) *node {
	r, _ := n.children.find(key)
	return r
}

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
		// Don't record a match for a nameless wildcard (which arises from a trailing slash).
		if c.pattern.lastSegment().s != "" {
			matches = append(matches, path[1:]) // remove initial slash
		}
		return c, matches
	}
	return nil, nil
}
