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

	for _, test := range []struct {
		method      string
		url         string
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
		{"CONNECT", "//", fmt.Sprintf("%#v", http.NotFoundHandler())},
		{"CONNECT", "/foo/../bar/./..//baz", "&muxpatterns.handler{i:2}"},
		{"CONNECT", "/foo", "&muxpatterns.handler{i:3}"},
		{"CONNECT", "/foo/x", "&muxpatterns.handler{i:2}"},
		{"CONNECT", "/bar/x", "&muxpatterns.handler{i:4}"},
		{"CONNECT", "/bar", `&http.redirectHandler{url:"/bar/", code:301}`},
	} {
		var r http.Request
		r.Method = test.method
		r.Host = "example.com"
		u, err := url.Parse(test.url)
		if err != nil {
			t.Fatal(err)
		}
		r.URL = u
		gotH, _, _ := mux.handler(&r)
		got := fmt.Sprintf("%#v", gotH)
		if got != test.wantHandler {
			t.Errorf("%s %q: got %q, want %q", test.method, test.url, got, test.wantHandler)
		}

		hh, _ := hmux.Handler(&r)
		hhs := fmt.Sprintf("%#v", hh)
		if hhs != test.wantHandler {
			t.Errorf("%s %q: http: got %s, want %s\n", test.method, test.url, hhs, test.wantHandler)
		}

	}
}
