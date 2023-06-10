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

func (mux *ServeMux) findHandler(method, host, path string) (h http.Handler, pattern string, multi bool, matches []string) {
	mux.mu.RLock()
	defer mux.mu.RUnlock()
	n, matches := mux.tree.match(method, host, path)
	if n == nil {
		return http.NotFoundHandler(), "", false, nil
	}
	segs := n.pattern.segments
	multi = segs[len(segs)-1].multi
	return n.handler, n.pattern.String(), multi, matches
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
		if u, ok := mux.redirectToPathSlash(r.Method, r.URL.Host, r.URL.Path, r.URL); ok {
			return http.RedirectHandler(u.String(), http.StatusMovedPermanently), u.Path, nil
		}

		h, p, _, m := mux.findHandler(r.Method, r.Host, r.URL.Path)
		return h, p, m
	}

	// All other requests have any port stripped and path cleaned
	// before passing to mux.handler.
	host := stripHostPort(r.Host)
	path := cleanPath(r.URL.Path)

	// If the given path is /tree and its handler is not registered,
	// redirect for /tree/.
	if u, ok := mux.redirectToPathSlash(r.Method, host, path, r.URL); ok {
		return http.RedirectHandler(u.String(), http.StatusMovedPermanently), u.Path, nil
	}

	if path != r.URL.Path {
		_, pattern, _, _ = mux.findHandler(r.Method, host, path)
		u := &url.URL{Path: path, RawQuery: r.URL.RawQuery}
		return http.RedirectHandler(u.String(), http.StatusMovedPermanently), pattern, nil
	}
	h, p, _, m := mux.findHandler(r.Method, host, r.URL.Path)
	return h, p, m
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

// redirectToPathSlash determines if the given path needs appending "/" to it.
// This occurs when a handler for path + "/" was already registered, but
// not for path itself. If the path needs appending to, it creates a new
// URL, setting the path to u.Path + "/" and returning true to indicate so.
func (mux *ServeMux) redirectToPathSlash(method, host, path string, u *url.URL) (*url.URL, bool) {
	// BEGIN CHANGE
	if len(path) == 0 || path[len(path)-1] == '/' {
		return u, false
	}
	// END CHANGE
	mux.mu.RLock()
	shouldRedirect := mux.shouldRedirectRLocked(method, host, path)
	mux.mu.RUnlock()
	if !shouldRedirect {
		return u, false
	}
	path = path + "/"
	u = &url.URL{Path: path, RawQuery: u.RawQuery}
	return u, true
}

func (mux *ServeMux) shouldRedirectRLocked(method, host, path string) bool {
	// BEGIN CHANGE
	// TODO: This is a second lookup for every matching path that doesn't end in '/'. Avoid.
	// If path matches a pattern exactly, not via a multi, then don't redirect.
	if n, _ := mux.tree.match(method, host, path); n != nil {
		segs := n.pattern.segments
		if !segs[len(segs)-1].multi {
			return false
		}
	}
	// TODO: this will succeed if "/path/{$}" is registered, as well as "/path/". Do we want that?
	// If not, it's not so easy to fix: if both are registered, the first will be returned in this
	// case, masking the second; and the redirect will find the first. I don't think we can
	// do anything about this.
	n, _ := mux.tree.match(method, host, path+"/")
	nSegs := strings.Count(path, "/")
	return n != nil && len(n.pattern.segments) > nSegs
	// END CHANGE
}
