// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements a decision tree for fast
// matching of requests to patterns.

package muxpatterns

import (
	"net/http"
	"net/url"
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
	//     "/"	trailing slash (resulting from {$})
	//	   ""   single wildcard
	//	   "*"  multi wildcard
	children   mapping[string, *node]
	emptyChild *node // optimization: child with key ""
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

// If method is non-empty, match returns the leaf node that matches the
// arguments, and a list of values for pattern wildcards in the order that the
// wildcards appear.
//
// If method is empty, the
func (root *node) match(method, host, path string) (*node, []string) {
	if host != "" {
		// There is a host. If there is a pattern that specifies that host and it
		// matches, we are done. If the pattern doesn't match, fall through to
		// try patterns with no host.
		if p, m := root.findChild(host).matchMethodAndPath(method, path); p != nil {
			return p, m
		}
	}
	return root.emptyChild.matchMethodAndPath(method, path)
}

func (n *node) matchMethodAndPath(method, path string) (*node, []string) {
	if n == nil {
		return nil, nil
	}
	if p, m := n.findChild(method).matchPath(path, nil); p != nil {
		// Exact match of method name.
		return p, m
	}
	if method == "HEAD" {
		// GET matches HEAD too.
		if p, m := n.findChild("GET").matchPath(path, nil); p != nil {
			return p, m
		}
	}
	return n.emptyChild.matchPath(path, nil)
}

func (n *node) matchPath(path string, matches []string) (*node, []string) {
	if n == nil {
		return nil, nil
	}
	// If path is empty, then return the node, whose pattern may be nil.
	if path == "" {
		if n.pattern == nil {
			return nil, nil
		}
		return n, matches
	}
	seg, rest := nextSegment(path)
	// Match literal.
	if n, m := n.findChild(seg).matchPath(rest, matches); n != nil {
		return n, m
	}
	// Match single wildcard, but not on a trailing slash.
	if seg != "/" {
		if n, m := n.emptyChild.matchPath(rest, append(matches, matchValue(seg))); n != nil {
			return n, m
		}
	}
	// Match multi wildcard to the rest of the pattern.
	if c := n.findChild("*"); c != nil {
		// Don't record a match for a nameless wildcard (which arises from a
		// trailing slash in the pattern).
		if c.pattern.lastSegment().s != "" {
			matches = append(matches, matchValue(path[1:])) // remove initial slash
		}
		return c, matches
	}
	return nil, nil
}

// matchingMethods returns a sorted list of all methods that, if passed to node.match
// with the given host and path, would result in a match.
func (root *node) matchingMethods(host, path string, methodSet map[string]bool) {
	if host != "" {
		root.findChild(host).matchingMethodsPath(path, methodSet)
	}
	root.emptyChild.matchingMethodsPath(path, methodSet)
	if methodSet["GET"] {
		methodSet["HEAD"] = true
	}
}

func (n *node) matchingMethodsPath(path string, set map[string]bool) {
	if n == nil {
		return
	}
	n.children.pairs(func(method string, c *node) bool {
		if p, _ := c.matchPath(path, nil); p != nil {
			set[method] = true
		}
		return true
	})
	// Don't look at the empty child. If there were an empty
	// child, it would match on any method, but we only
	// call this when we fail to match on a method.
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

func matchValue(path string) string {
	m, err := url.PathUnescape(path)
	if err != nil {
		// Path is not properly escaped, so use the original.
		return path
	}
	return m
}
