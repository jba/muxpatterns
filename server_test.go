// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	mux.Handle("//foo", &handler{5})

	hmux := http.NewServeMux()
	hmux.Handle("/", &handler{1})
	hmux.Handle("/foo/", &handler{2})
	hmux.Handle("/foo", &handler{3})
	hmux.Handle("/bar/", &handler{4})
	hmux.Handle("//foo", &handler{5})

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
		{"CONNECT", "//foo", "&muxpatterns.handler{i:5}"},
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
		gotH, _, _, _ := mux.handler(&r)
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

func TestPathValue(t *testing.T) {
	for _, test := range []struct {
		pattern string
		url     string
		want    map[string]string
	}{
		{
			"/{a}/is/{b}/{c...}",
			"/now/is/the/time/for/all",
			map[string]string{
				"a": "now",
				"b": "the",
				"c": "time/for/all",
				"d": "",
			},
		},
		{
			"/names/{name}/{other...}",
			"/names/" + url.PathEscape("/john") + "/address",
			map[string]string{
				"name":  "/john",
				"other": "address",
			},
		},
		{
			"/names/{name}/{other...}",
			"/names/" + url.PathEscape("john/doe") + "/address",
			map[string]string{
				"name":  "john/doe",
				"other": "address",
			},
		},
	} {
		mux := NewServeMux()
		mux.Handle(test.pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for name, want := range test.want {
				got := mux.PathValue(r, name)
				if got != want {
					t.Errorf("%q, %q: got %q, want %q", test.pattern, name, got, want)
				}
			}
		}))
		server := httptest.NewServer(mux)
		defer server.Close()
		_, err := http.Get(server.URL + test.url)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSetPathValue(t *testing.T) {
	mux := NewServeMux()
	mux.Handle("/a/{b}/c/{d...}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.SetPathValue(r, "b", "X")
		mux.SetPathValue(r, "d", "Y")
		mux.SetPathValue(r, "a", "ignored")

		if g, w := mux.PathValue(r, "b"), "X"; g != w {
			t.Errorf("got %q, want %q", g, w)
		}
		if g, w := mux.PathValue(r, "d"), "Y"; g != w {
			t.Errorf("got %q, want %q", g, w)
		}
	}))
	server := httptest.NewServer(mux)
	defer server.Close()
	_, err := http.Get(server.URL + "/a/b/c/d/e")
	if err != nil {
		t.Fatal(err)
	}
}

func TestEscapedPath(t *testing.T) {
	mux := NewServeMux()
	var gotPattern, gotMatch string
	pat1 := "/a/b/c"
	mux.Handle(pat1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPattern = pat1
		gotMatch = ""
	}))
	pat2 := "/{x}/c"
	mux.Handle(pat2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPattern = pat2
		gotMatch = mux.PathValue(r, "x")
	}))

	server := httptest.NewServer(mux)
	defer server.Close()

	for _, test := range []struct {
		path        string
		wantPattern string
		wantMatch   string
	}{
		{"/a/b/c", pat1, ""},
		{"/a%2Fb/c", pat2, "a/b"},
	} {
		gotPattern = ""
		gotMatch = ""
		res, err := http.Get(server.URL + test.path)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 {
			t.Errorf("got code %d, want 200", res.StatusCode)
		}
		if g, w := gotPattern, test.wantPattern; g != w {
			t.Errorf("pattern: got %q, want %q", g, w)
		}
		if g, w := gotMatch, test.wantMatch; g != w {
			t.Errorf("match: got %q, want %q", g, w)
		}
	}
}

func TestStatus(t *testing.T) {
	mux := NewServeMux()
	mux.Handle("GET /g", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	mux.Handle("POST /p", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server := httptest.NewServer(mux)
	defer server.Close()

	for _, test := range []struct {
		path string
		// method is always GET
		want int
	}{
		{"/g", 200},
		{"/x", 404},
		{"/p", 405}, // path matches a different method
		{"/./p", 405},
	} {
		res, err := http.Get(server.URL + test.path)
		if err != nil {
			t.Fatal(err)
		}
		if g, w := res.StatusCode, test.want; g != w {
			t.Errorf("got %d, want %d", g, w)
		}
	}
}

func BenchmarkRegister(b *testing.B) {
	f, err := os.Open(filepath.Join("testdata", "patterns.txt"))
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	var patterns []string
	for scan.Scan() {
		pat := scan.Text()
		if len(pat) == 0 || pat[0] == '#' {
			continue
		}
		patterns = append(patterns, pat)
	}
	if scan.Err() != nil {
		b.Fatal(scan.Err())
	}
	b.Logf("benchmarking with %d patterns", len(patterns))
	// To make path comparison harder, move API name to the end.
	for i, p := range patterns {
		patterns[i] = moveFirstSegmentToEnd(p)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mux := NewServeMux()
		for _, p := range patterns {
			if err := mux.register(p, http.NotFoundHandler()); err != nil {
				b.Fatal(err)
			}
		}
		b.Logf("conflict calls: %d", mux.conflictCalls.Load())
	}
}

func moveFirstSegmentToEnd(pat string) string {
	method, path, found := strings.Cut(pat, " ")
	if !found {
		panic("bad pattern:" + pat)
	}
	if len(path) == 0 || path[0] != '/' {
		panic("bad path: " + path)
	}
	first, rest, found := strings.Cut(path[1:], "/")
	if !found {
		panic("missing /:" + path[1:])
	}
	path = "/" + rest + "/" + first
	return method + " " + path
}

// Benchmark for finding a handler from a request.
// All these patterns are static so we can compare with net/http.ServeMux.
// The patterns come from github.com/julienschmidt/go-http-routing-benchmark.
func BenchmarkServeHTTP(b *testing.B) {
	patterns := []string{
		"/",
		"/cmd.html",
		"/code.html",
		"/contrib.html",
		"/contribute.html",
		"/debugging_with_gdb.html",
		"/docs.html",
		"/effective_go.html",
		"/files.log",
		"/gccgo_contribute.html",
		"/gccgo_install.html",
		"/go-logo-black.png",
		"/go-logo-blue.png",
		"/go-logo-white.png",
		"/go1.1.html",
		"/go1.2.html",
		"/go1.html",
		"/go1compat.html",
		"/go_faq.html",
		"/go_mem.html",
		"/go_spec.html",
		"/help.html",
		"/ie.css",
		"/install-source.html",
		"/install.html",
		"/logo-153x55.png",
		"/Makefile",
		"/root.html",
		"/share.png",
		"/sieve.gif",
		"/tos.html",
		"/articles",
		"/articles/go_command.html",
		"/articles/index.html",
		"/articles/wiki",
		"/articles/wiki/edit.html",
		"/articles/wiki/final-noclosure.go",
		"/articles/wiki/final-noerror.go",
		"/articles/wiki/final-parsetemplate.go",
		"/articles/wiki/final-template.go",
		"/articles/wiki/final.go",
		"/articles/wiki/get.go",
		"/articles/wiki/http-sample.go",
		"/articles/wiki/index.html",
		"/articles/wiki/Makefile",
		"/articles/wiki/notemplate.go",
		"/articles/wiki/part1-noerror.go",
		"/articles/wiki/part1.go",
		"/articles/wiki/part2.go",
		"/articles/wiki/part3-errorhandling.go",
		"/articles/wiki/part3.go",
		"/articles/wiki/test.bash",
		"/articles/wiki/test_edit.good",
		"/articles/wiki/test_Test.txt.good",
		"/articles/wiki/test_view.good",
		"/articles/wiki/view.html",
		"/codewalk",
		"/codewalk/codewalk.css",
		"/codewalk/codewalk.js",
		"/codewalk/codewalk.xml",
		"/codewalk/functions.xml",
		"/codewalk/markov.go",
		"/codewalk/markov.xml",
		"/codewalk/pig.go",
		"/codewalk/popout.png",
		"/codewalk/run",
		"/codewalk/sharemem.xml",
		"/codewalk/urlpoll.go",
		"/devel",
		"/devel/release.html",
		"/devel/weekly.html",
		"/gopher",
		"/gopher/appenginegopher.jpg",
		"/gopher/appenginegophercolor.jpg",
		"/gopher/appenginelogo.gif",
		"/gopher/bumper.png",
		"/gopher/bumper192x108.png",
		"/gopher/bumper320x180.png",
		"/gopher/bumper480x270.png",
		"/gopher/bumper640x360.png",
		"/gopher/doc.png",
		"/gopher/frontpage.png",
		"/gopher/gopherbw.png",
		"/gopher/gophercolor.png",
		"/gopher/gophercolor16x16.png",
		"/gopher/help.png",
		"/gopher/pkg.png",
		"/gopher/project.png",
		"/gopher/ref.png",
		"/gopher/run.png",
		"/gopher/talks.png",
		"/gopher/pencil",
		"/gopher/pencil/gopherhat.jpg",
		"/gopher/pencil/gopherhelmet.jpg",
		"/gopher/pencil/gophermega.jpg",
		"/gopher/pencil/gopherrunning.jpg",
		"/gopher/pencil/gopherswim.jpg",
		"/gopher/pencil/gopherswrench.jpg",
		"/play",
		"/play/fib.go",
		"/play/hello.go",
		"/play/life.go",
		"/play/peano.go",
		"/play/pi.go",
		"/play/sieve.go",
		"/play/solitaire.go",
		"/play/tree.go",
		"/progs",
		"/progs/cgo1.go",
		"/progs/cgo2.go",
		"/progs/cgo3.go",
		"/progs/cgo4.go",
		"/progs/defer.go",
		"/progs/defer.out",
		"/progs/defer2.go",
		"/progs/defer2.out",
		"/progs/eff_bytesize.go",
		"/progs/eff_bytesize.out",
		"/progs/eff_qr.go",
		"/progs/eff_sequence.go",
		"/progs/eff_sequence.out",
		"/progs/eff_unused1.go",
		"/progs/eff_unused2.go",
		"/progs/error.go",
		"/progs/error2.go",
		"/progs/error3.go",
		"/progs/error4.go",
		"/progs/go1.go",
		"/progs/gobs1.go",
		"/progs/gobs2.go",
		"/progs/image_draw.go",
		"/progs/image_package1.go",
		"/progs/image_package1.out",
		"/progs/image_package2.go",
		"/progs/image_package2.out",
		"/progs/image_package3.go",
		"/progs/image_package3.out",
		"/progs/image_package4.go",
		"/progs/image_package4.out",
		"/progs/image_package5.go",
		"/progs/image_package5.out",
		"/progs/image_package6.go",
		"/progs/image_package6.out",
		"/progs/interface.go",
		"/progs/interface2.go",
		"/progs/interface2.out",
		"/progs/json1.go",
		"/progs/json2.go",
		"/progs/json2.out",
		"/progs/json3.go",
		"/progs/json4.go",
		"/progs/json5.go",
		"/progs/run",
		"/progs/slices.go",
		"/progs/timeout1.go",
		"/progs/timeout2.go",
		"/progs/update.bash",
	}

	r, _ := http.NewRequest("GET", "/", nil)
	u := r.URL
	rq := u.RawQuery
	w := new(mockResponseWriter)

	b.Run("http", func(b *testing.B) {
		s := http.NewServeMux()
		for _, p := range patterns {
			s.HandleFunc(p, httpHandlerFunc)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, p := range patterns {
				r.RequestURI = p
				u.Path = p
				u.RawQuery = rq
				s.ServeHTTP(w, r)
			}
		}
	})
	b.Run("muxpatterns", func(b *testing.B) {
		s := NewServeMux()
		for _, p := range patterns {
			s.HandleFunc(p, httpHandlerFunc)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, p := range patterns {
				r.RequestURI = p
				u.Path = p
				u.RawQuery = rq
				s.ServeHTTP(w, r)
			}
		}
	})
}

func httpHandlerFunc(_ http.ResponseWriter, _ *http.Request) {}

type mockResponseWriter struct{}

func (m *mockResponseWriter) Header() (h http.Header) {
	return http.Header{}
}

func (m *mockResponseWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (m *mockResponseWriter) WriteString(s string) (n int, err error) {
	return len(s), nil
}

func (m *mockResponseWriter) WriteHeader(int) {}
