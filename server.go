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
)

// ServeMux is an HTTP request multiplexer.
// It behaves like [net/http.ServeMux], but using the enhanced patterns
// of this package.
type ServeMux struct {
	mu   sync.RWMutex
	tree *node
}

func NewServeMux() *ServeMux {
	return &ServeMux{}
}

func (mux *ServeMux) Handle(pattern string, handler http.Handler) {
	if err := mux.register(pattern, handler); err != nil {
		panic(err)
	}
}

func (mux *ServeMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	// Does not call Handle so that we retrieve the right source location in ServeMux.register.
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
	loc := callerLocation()
	mux.mu.Lock()
	defer mux.mu.Unlock()
	pat, err := Parse(pattern)
	if err != nil {
		return err
	}
	// Check for conflict.
	if err := mux.tree.patterns(func(pat2 *Pattern, _ http.Handler, loc2 string) error {
		if pat.ConflictsWith(pat2) {
			d := describeRel(pat, pat2)
			return fmt.Errorf("pattern %q (registered at %s) conflicts with pattern %q (registered at %s):\n%s",
				pat, loc, pat2, loc2, d)
		}
		return nil
	}); err != nil {
		return err
	}
	if mux.tree == nil {
		mux.tree = &node{}
	}
	mux.tree.addPattern(pat, handler, loc)
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
	h, p, _ := mux.handler(r)
	return h, p
}

func (p *Pattern) isMulti() bool {
	return p.segments[len(p.segments)-1].multi
}

func handlerResult(n *node, matches []string) (h http.Handler, pattern string, ms []string) {
	if n == nil {
		return http.NotFoundHandler(), "", nil
	}
	return n.handler, n.pattern.String(), matches
}

// func (mux *ServeMux) findHandler(method, host, path string) (h http.Handler, pattern string, matches []string) {
// 	n, matches := mux.match(method, host, path)
// 	if n == nil {
// 		return http.NotFoundHandler(), "", nil
// 	}
// 	return n.handler, n.pattern.String(), matches
// }

func (mux *ServeMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// This if statement copied from net/http/server.go.
	if r.RequestURI == "*" {
		if r.ProtoAtLeast(1, 1) {
			w.Header().Set("Connection", "close")
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	h, _, matches := mux.handler(r)
	_ = matches // TODO
	h.ServeHTTP(w, r)
}

func (mux *ServeMux) handler(r *http.Request) (h http.Handler, pattern string, matches []string) {
	// CONNECT requests are not canonicalized.
	if r.Method == "CONNECT" {
		// If r.URL.Path is /tree and its handler is not registered,
		// the /tree -> /tree/ redirect applies to CONNECT requests
		// but the path canonicalization does not.
		n, matches, u, redirect := mux.matchOrRedirect(r.Method, r.URL.Host, r.URL.Path, r.URL)
		if redirect {
			return http.RedirectHandler(u.String(), http.StatusMovedPermanently), u.Path, nil
		}
		// Redo the match, this time with r.Host instead of r.URL.Host.
		// Pass a nil URL to skip the trailing-slash redirect logic.
		n, matches, _, _ = mux.matchOrRedirect(r.Method, r.Host, r.URL.Path, nil)
		return handlerResult(n, matches)
	}

	// All other requests have any port stripped and path cleaned
	// before passing to mux.handler.
	host := stripHostPort(r.Host)
	path := cleanPath(r.URL.Path)

	// If the given path is /tree and its handler is not registered,
	// redirect for /tree/.
	n, matches, u, redirect := mux.matchOrRedirect(r.Method, host, path, r.URL)
	if redirect {
		return http.RedirectHandler(u.String(), http.StatusMovedPermanently), u.Path, nil
	}
	if path != r.URL.Path {
		// Redirect to cleaned path.
		pattern := ""
		if n != nil {
			pattern = n.pattern.String()
		}
		u := &url.URL{Path: path, RawQuery: r.URL.RawQuery}
		return http.RedirectHandler(u.String(), http.StatusMovedPermanently), pattern, nil
	}
	return handlerResult(n, matches)
}

// cleanPath returns the canonical path for p, eliminating . and .. elements.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
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

func exactMatch(n *node, path string) bool {
	if n == nil {
		return false
	}
	if len(path) > 0 && path[len(path)-1] != '/' {
		// If the path doesn't end in a trailing slash, then
		// an exact match is one that doesn't end in a multi.
		return !n.pattern.isMulti()
	}
	// Only patterns ending in {$} or a multi wildcard can
	// match a path with a trailing slash.
	// For the match to be exact, the number of pattern
	// segments should be the same as the number of slashes in the path.
	// E.g. "/a/b/{$}" and "/a/b/{...}" exactly match "/a/b/", but "/a/" does not.
	return len(n.pattern.segments) == strings.Count(path, "/")
}
