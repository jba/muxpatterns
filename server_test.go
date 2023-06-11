// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

type handler struct{ i int }

func (handler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestServeMuxHandler(t *testing.T) {
	mux := NewServeMux()
	mux.Handle("/", &handler{1})
	mux.Handle("/foo/", &handler{2})
	mux.Handle("/foo", &handler{3})
	mux.Handle("/bar/", &handler{4})

	hmux := http.NewServeMux()
	hmux.Handle("/", &handler{1})
	hmux.Handle("/foo/", &handler{2})
	hmux.Handle("/foo", &handler{3})
	hmux.Handle("/bar/", &handler{4})
	// TODO: add this to mux, too, once we relax pattern parsing.
	//hmux.Handle("//foo", &handler{5})

	for _, test := range []struct {
		method      string
		path        string
		wantHandler string
	}{
		{"GET", "/", "&muxpatterns.handler{i:1}"},
		{"GET", "//", `&http.redirectHandler{url:"/", code:301}`},
		{"GET", "/foo/../bar/./..//baz", `&http.redirectHandler{url:"/baz", code:301}`},
		{"GET", "/foo", "&muxpatterns.handler{i:3}"},
		{"GET", "/foo/x", "&muxpatterns.handler{i:2}"},
		{"GET", "/bar/x", "&muxpatterns.handler{i:4}"},
		{"GET", "/bar", `&http.redirectHandler{url:"/bar/", code:301}`},
		{"CONNECT", "/", "&muxpatterns.handler{i:1}"},
		{"CONNECT", "//", "&muxpatterns.handler{i:1}"},
		{"CONNECT", "//foo", "&muxpatterns.handler{i:1}"},
		{"CONNECT", "/foo/../bar/./..//baz", "&muxpatterns.handler{i:2}"},
		{"CONNECT", "/foo", "&muxpatterns.handler{i:3}"},
		{"CONNECT", "/foo/x", "&muxpatterns.handler{i:2}"},
		{"CONNECT", "/bar/x", "&muxpatterns.handler{i:4}"},
		{"CONNECT", "/bar", `&http.redirectHandler{url:"/bar/", code:301}`},
	} {
		var r http.Request
		r.Method = test.method
		r.Host = "example.com"
		r.URL = &url.URL{Path: test.path}
		gotH, _, _ := mux.handler(&r)
		got := fmt.Sprintf("%#v", gotH)
		if got != test.wantHandler {
			t.Errorf("%s %q: got %q, want %q", test.method, test.path, got, test.wantHandler)
		}

		hh, _ := hmux.Handler(&r)
		hhs := fmt.Sprintf("%#v", hh)
		if hhs != test.wantHandler {
			t.Errorf("%s %q: http: got %s, want %s\n", test.method, test.path, hhs, test.wantHandler)
		}

	}
}

func TestServeMuxBadURLs(t *testing.T) {
	hmux := http.NewServeMux()
	hmux.Handle("/", &handler{1})
	hmux.Handle("/foo", &handler{2})
	hmux.Handle("/foo/../bar", &handler{3})

	r, err := http.NewRequest(http.MethodConnect, "/foo/../bar", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, gotpat := hmux.Handler(r)
	fmt.Printf("%#v, %q\n", got, gotpat)
}

func TestExactMatch(t *testing.T) {
	for _, test := range []struct {
		pattern string
		path    string
		want    bool
	}{
		{"", "/a", false},
		{"/", "/a", false},
		{"/a", "/a", true},
		{"/a/{x...}", "/a/b", false},
		{"/a/{x}", "/a/b", true},
		{"/a/b/", "/a/b/", true},
		{"/a/b/{$}", "/a/b/", true},
		{"/a/", "/a/b/", false},
	} {
		var n *node
		if test.pattern != "" {
			pat, err := Parse(test.pattern)
			if err != nil {
				t.Fatal(err)
			}
			n = &node{pattern: pat}
		}
		got := exactMatch(n, test.path)
		if got != test.want {
			t.Errorf("%q, %s: got %t, want %t", test.pattern, test.path, got, test.want)
		}
	}
}
