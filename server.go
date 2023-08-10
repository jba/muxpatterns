// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// ServeMux and related code.
// Much of this is copied from net/http/server.go.

package muxpatterns

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// ServeMux is an HTTP request multiplexer.
// It behaves like [net/http.ServeMux], but using the enhanced patterns
// of this package.
type ServeMux struct {
	mu   sync.RWMutex
	tree *node
	// Temporary hack to expose pattern matches.
	// This grows without bound!
	matches       map[*http.Request]*match
	conflictCalls atomic.Int32
	index         *index
}

func NewServeMux() *ServeMux {
	return &ServeMux{
		tree:    &node{},
		matches: map[*http.Request]*match{},
		index:   newIndex(),
	}
}

func (mux *ServeMux) Handle(pattern string, handler http.Handler) {
	if err := mux.register(pattern, handler); err != nil {
		panic(err)
	}
}

func (mux *ServeMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	// Does not call Handle so that  ServeMux.register retrieves the right source location.
	if err := mux.register(pattern, http.HandlerFunc(handler)); err != nil {
		panic(err)
	}
}

func (mux *ServeMux) register(pattern string, handler http.Handler) error {
	if pattern == "" {
		return errors.New("http: invalid pattern")
	}
	if handler == nil {
		return errors.New("http: nil handler")
	}

	pat, err := Parse(pattern)
	if err != nil {
		return err
	}
	pat.loc = callerLocation()
	mux.mu.Lock()
	defer mux.mu.Unlock()
	// Check for conflict.
	npats := 0
	if err := mux.index.possiblyConflictingPatterns(pat, func(pat2 *Pattern) error {
		npats++
		mux.conflictCalls.Add(1)
		if pat.ConflictsWith(pat2) {
			d := describeRel(pat, pat2)
			return fmt.Errorf("pattern %q (registered at %s) conflicts with pattern %q (registered at %s):\n%s",
				pat, pat.loc, pat2, pat2.loc, d)
		}
		return nil
	}); err != nil {
		return err
	}
	mux.tree.addPattern(pat, handler)
	mux.index.addPattern(pat)
	return nil
}

func callerLocation() string {
	_, file, line, ok := runtime.Caller(2) // caller's caller's caller
	if !ok {
		return "unknown location"
	}
	return fmt.Sprintf("%s:%d", file, line)
}

func (mux *ServeMux) Handler(r *http.Request) (h http.Handler, pattern string) {
	h, _, sp, _ := mux.handler(r)
	return h, sp
}

func (mux *ServeMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// This if statement copied from net/http/server.go.
	if r.RequestURI == "*" {
		if r.ProtoAtLeast(1, 1) {
			w.Header().Set("Connection", "close")
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	h, pat, _, matches := mux.handler(r)
	if pat != nil && matches != nil {
		mux.mu.Lock()
		mux.matches[r] = &match{pat: pat, values: matches}
		mux.mu.Unlock()
	}
	h.ServeHTTP(w, r)
}

func (mux *ServeMux) handler(r *http.Request) (h http.Handler, pattern *Pattern, spat string, matches []string) {
	var (
		n        *node
		u        *url.URL
		redirect bool
		host     string
		path     string
	)
	host = r.URL.Host
	escapedPath := r.URL.EscapedPath()
	path = escapedPath
	// CONNECT requests are not canonicalized.
	if r.Method == "CONNECT" {
		// If r.URL.Path is /tree and its handler is not registered,
		// the /tree -> /tree/ redirect applies to CONNECT requests
		// but the path canonicalization does not.
		_, _, u, redirect = mux.matchOrRedirect(r.Method, host, path, r.URL)
		if redirect {
			return http.RedirectHandler(u.String(), http.StatusMovedPermanently), nil, u.Path, nil
		}
		// Redo the match, this time with r.Host instead of r.URL.Host.
		// Pass a nil URL to skip the trailing-slash redirect logic.
		n, matches, _, _ = mux.matchOrRedirect(r.Method, r.Host, path, nil)
	} else {
		// All other requests have any port stripped and path cleaned
		// before passing to mux.handler.
		host = stripHostPort(r.Host)
		path = cleanPath(path)

		// If the given path is /tree and its handler is not registered,
		// redirect for /tree/.
		n, matches, u, redirect = mux.matchOrRedirect(r.Method, host, path, r.URL)
		if redirect {
			return http.RedirectHandler(u.String(), http.StatusMovedPermanently), nil, u.Path, nil
		}
		if path != escapedPath {
			// Redirect to cleaned path.
			pattern := ""
			if n != nil {
				pattern = n.pattern.String()
			}
			u := &url.URL{Path: path, RawQuery: r.URL.RawQuery}
			return http.RedirectHandler(u.String(), http.StatusMovedPermanently), nil, pattern, nil
		}
	}
	if n == nil {
		// We didn't find a match with the request method. To distinguish between
		// Not Found and Method Not Allowed, see if there is another pattern that
		// matches except for the method.
		if m, _, _, _ := mux.matchOrRedirect("", host, path, r.URL); m != nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			}), nil, "", nil
		}
		return http.NotFoundHandler(), nil, "", nil
	}
	return n.handler, n.pattern, n.pattern.String(), matches
}

func mightNeedCleaning(p string) bool {
	var prev byte = ' '
	for i := 0; i < len(p); i++ {
		c := p[i]
		if prev == '/' && (c == '/' || c == '.') {
			return true
		}
		prev = c
	}
	return false
}

// cleanPath returns the canonical path for p, eliminating . and .. elements.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}

	if !mightNeedCleaning(p) {
		return p
	}

	np := path.Clean(p)
	// path.Clean removes trailing slash except for root;
	// put the trailing slash back if necessary.
	if p[len(p)-1] == '/' && np != "/" {
		// Fast path for common case of p being the string we want:
		if len(p) == len(np)+1 && strings.HasPrefix(p, np) {
			np = p
		} else {
			np += "/"
		}
	}
	return np
}

// stripHostPort returns h without any trailing ":<port>".
func stripHostPort(h string) string {
	// If no port on host, return unchanged
	if !strings.Contains(h, ":") {
		return h
	}
	host, _, err := net.SplitHostPort(h)
	if err != nil {
		return h // on error, return unchanged
	}
	return host
}

func (mux *ServeMux) matchOrRedirect(method, host, path string, u *url.URL) (*node, []string, *url.URL, bool) {
	// Hold the read lock for the entire method so that the two matches are done
	// on the same set of registered patterns.
	mux.mu.RLock()
	defer mux.mu.RUnlock()
	n, matches := mux.tree.match(method, host, path)
	// If we have an exact match, then don't redirect.
	if !exactMatch(n, path) && u != nil {
		// If there is an exact match with a trailing slash, then redirect.
		path += "/"
		n2, _ := mux.tree.match(method, host, path)
		if exactMatch(n2, path) {
			return nil, nil, &url.URL{Path: path, RawQuery: u.RawQuery}, true
		}
	}
	return n, matches, nil, false
}

// exactMatch reports whether the node's pattern exactly matches the path.
func exactMatch(n *node, path string) bool {
	if n == nil {
		return false
	}
	if len(path) > 0 && path[len(path)-1] != '/' {
		// If the path doesn't end in a trailing slash, then
		// an exact match is one that doesn't end in a multi.
		return !n.pattern.lastSegment().multi
	}
	// Only patterns ending in {$} or a multi wildcard can
	// match a path with a trailing slash.
	// For the match to be exact, the number of pattern
	// segments should be the same as the number of slashes in the path.
	// E.g. "/a/b/{$}" and "/a/b/{...}" exactly match "/a/b/", but "/a/" does not.
	return len(n.pattern.segments) == strings.Count(path, "/")
}

// PathValue returns the value for the named path wildcard in the
// pattern that matched the request.
// If there is no matched wildcard with the name, PathValue returns
// the empty string.
//
// This is a method on ServeMux only for demo purposes.
// In the actual implementation, it will be a method on Request.
func (mux *ServeMux) PathValue(r *http.Request, name string) string {
	mux.mu.RLock()
	defer mux.mu.RUnlock()
	return mux.matches[r].get(name)
}

// SetPathValue sets the value for path element name in r.
//
// This is a method on ServeMux only for demo purposes.
// In the actual implementation, it will be a method on Request.
func (mux *ServeMux) SetPathValue(r *http.Request, name, value string) {
	mux.mu.Lock()
	defer mux.mu.Unlock()
	m, ok := mux.matches[r]
	if !ok {
		m = &match{}
		mux.matches[r] = m
	}
	m.set(name, value)
}

type match struct {
	pat    *Pattern
	values []string
	other  map[string]string // for calls to SetPathValue that don't match a wildcard
}

func (m *match) get(name string) string {
	if m == nil {
		return ""
	}
	if i := m.index(name); i >= 0 {
		return m.values[i]
	}
	return m.other[name]
}

func (m *match) set(name, value string) {
	if i := m.index(name); i >= 0 {
		m.values[i] = value
		return
	}
	if m.other == nil {
		m.other = map[string]string{}
	}
	m.other[name] = value
}

func (m *match) index(name string) int {
	if m.pat == nil {
		return -1
	}
	i := 0
	for _, seg := range m.pat.segments {
		if seg.wild && seg.s != "" {
			if name == seg.s {
				return i
			}
			i++
		}
	}
	return -1
}
