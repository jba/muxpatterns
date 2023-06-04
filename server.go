// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The code in this file was copied from net/http/server.go.

package muxpatterns

import (
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
)

func (mux *ServeMux) handlerCopiedFromNetHTTP(r *http.Request) (h http.Handler, pattern string, matches []string) {

	// CONNECT requests are not canonicalized.
	if r.Method == "CONNECT" {
		// If r.URL.Path is /tree and its handler is not registered,
		// the /tree -> /tree/ redirect applies to CONNECT requests
		// but the path canonicalization does not.
		if u, ok := mux.redirectToPathSlash(r.Method, r.URL.Host, r.URL.Path, r.URL); ok {
			return http.RedirectHandler(u.String(), http.StatusMovedPermanently), u.Path, nil
		}

		return mux.handler(r.Method, r.Host, r.URL.Path)
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
		_, pattern, _ = mux.handler(r.Method, host, path)
		u := &url.URL{Path: path, RawQuery: r.URL.RawQuery}
		return http.RedirectHandler(u.String(), http.StatusMovedPermanently), pattern, nil
	}

	return mux.handler(r.Method, host, r.URL.Path)
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
	if n, _ := mux.tree.match(method, host, path); n != nil {
		return false
	}
	// TODO: this will succeed if "/path/{$}" is registered, as well as "/path/". Do we want that?
	// If not, it's not so easy to fix: if both are registered, the first will be returned in this
	// case, masking the second; and the redirect will find the first. I don't think we can
	// do anything about this.
	n, _ := mux.tree.match(method, host, path+"/")
	return n != nil
	// END CHANGE
}
