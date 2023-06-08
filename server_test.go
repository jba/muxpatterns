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

	for _, test := range []struct {
		url         string
		wantHandler string
	}{
		{"/", "&muxpatterns.handler{i:1}"},
		{"//", `&http.redirectHandler{url:"/", code:301}`},
		{"/foo/../bar/./..//baz", `&http.redirectHandler{url:"/baz", code:301}`},
		{"/foo", "&muxpatterns.handler{i:3}"},
		{"/foo/x", "&muxpatterns.handler{i:2}"},
		{"/bar/x", "&muxpatterns.handler{i:4}"},
		{"/bar", `&http.redirectHandler{url:"/bar/", code:301}`},
	} {
		var r http.Request
		r.Method = "GET"
		r.Host = "example.com"
		u, err := url.Parse(test.url)
		if err != nil {
			t.Fatal(err)
		}
		r.URL = u
		gotH, _, _ := mux.handler(&r)
		got := fmt.Sprintf("%#v", gotH)
		if got != test.wantHandler {
			t.Errorf("%q: got %q, want %q", test.url, got, test.wantHandler)
		}
	}
}
