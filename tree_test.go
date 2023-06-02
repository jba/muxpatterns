// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/exp/slices"
)

func TestNextSegment(t *testing.T) {
	for _, test := range []struct {
		in   string
		want []string
	}{
		{"/a/b/c", []string{"a", "b", "c"}},
		{"/a/b/", []string{"a", "b", "/"}},
		{"/", []string{"/"}},
	} {
		var got []string
		rest := test.in
		for len(rest) > 0 {
			var seg string
			seg, rest = nextSegment(rest)
			got = append(got, seg)
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("%q: got %v, want %v", test.in, got, test.want)
		}
	}
}

// TODO: test host and method
var testTree *node

func getTestTree() *node {
	if testTree == nil {
		initTestTree()
	}
	return testTree
}

func initTestTree() {
	testTree = &node{}
	var ps PatternSet
	for _, p := range []string{"/a", "/a/b", "/a/{x}",
		"/g/h/i", "/g/{x}/j",
		"/a/b/{x...}", "/a/b/{y}", "/a/b/{$}"} {
		pat, err := Parse(p)
		if err != nil {
			panic(err)
		}
		if err := ps.Register(pat); err != nil {
			panic(err)
		}
		testTree.addPattern(pat)
	}
}

func TestAddPattern(t *testing.T) {
	want := `nil
"":
    nil
    "":
        nil
        "a":
            "/a"
            "":
                "/a/{x}"
            "b":
                "/a/b"
                "":
                    "/a/b/{y}"
                "*":
                    "/a/b/{x...}"
                "/":
                    "/a/b/{$}"
        "g":
            nil
            "":
                nil
                "j":
                    "/g/{x}/j"
            "h":
                nil
                "i":
                    "/g/h/i"
`

	var b strings.Builder
	getTestTree().print(&b, 0)
	got := b.String()
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestNodeMatch(t *testing.T) {
	for _, test := range []struct {
		path        string
		wantPat     string // "" for nil
		wantMatches []string
	}{
		{"/a", "/a", nil},
		{"/b", "", nil},
		{"/a/b", "/a/b", nil},
		{"/a/c", "/a/{x}", []string{"c"}},
		{"/a/b/", "/a/b/{$}", nil},
		{"/a/b/c", "/a/b/{y}", []string{"c"}},
		{"/a/b/c/d", "/a/b/{x...}", []string{"c/d"}},
		{"/g/h/i", "/g/h/i", nil},
		{"/g/h/j", "/g/{x}/j", []string{"h"}},
	} {
		gotPat, gotMatches := getTestTree().match("GET", "", test.path)
		got := ""
		if gotPat != nil {
			got = gotPat.String()
		}
		if got != test.wantPat {
			t.Errorf("%s: got %q, want %q", test.path, got, test.wantPat)
		}
		if !slices.Equal(gotMatches, test.wantMatches) {
			t.Errorf("%s: got matches %v, want %v", test.path, gotMatches, test.wantMatches)
		}
	}
}

func findChildLinear(key string, entries []entry) *node {
	for _, e := range entries {
		if key == e.key {
			return e.child
		}
	}
	return nil
}

func BenchmarkFindChild(b *testing.B) {
	key := "articles"
	children := []string{
		"*",
		"cmd.html",
		"code.html",
		"contrib.html",
		"contribute.html",
		"debugging_with_gdb.html",
		"docs.html",
		"effective_go.html",
		"files.log",
		"gccgo_contribute.html",
		"gccgo_install.html",
		"go-logo-black.png",
		"go-logo-blue.png",
		"go-logo-white.png",
		"go1.1.html",
		"go1.2.html",
		"go1.html",
		"go1compat.html",
		"go_faq.html",
		"go_mem.html",
		"go_spec.html",
		"help.html",
		"ie.css",
		"install-source.html",
		"install.html",
		"logo-153x55.png",
		"Makefile",
		"root.html",
		"share.png",
		"sieve.gif",
		"tos.html",
		"articles",
	}
	if len(children) != 32 {
		panic("bad len")
	}
	for _, n := range []int{2, 4, 8, 16, 32} {
		list := children[:n]
		b.Run(strconv.Itoa(n), func(b *testing.B) {

			b.Run("linear", func(b *testing.B) {
				var entries []entry
				for _, c := range list {
					entries = append(entries, entry{c, nil})
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					findChildLinear(key, entries)
				}
			})
			b.Run("map", func(b *testing.B) {
				m := map[string]*node{}
				for _, c := range list {
					m[c] = nil
				}
				var x *node
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					x = m[key]
				}
				_ = x
			})
			b.Run("hybrid8", func(b *testing.B) {
				h := newHybrid(8)
				for _, c := range list {
					h.add(c, nil)
				}
				var x *node
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					x = h.get(key)
				}
				_ = x
			})
		})
	}
}

func TestHybrid(t *testing.T) {
	nodes := []*node{&node{}, &node{}, &node{}, &node{}, &node{}}
	h := newHybrid(4)
	for i := 0; i < 4; i++ {
		h.add(strconv.Itoa(i), nodes[i])
	}
	if h.m != nil {
		t.Fatal("h.m != nil")
	}
	for i := 0; i < 4; i++ {
		g := h.get(strconv.Itoa(i))
		if g != nodes[i] {
			t.Fatalf("%d: different", i)
		}
	}
	h.add("4", nodes[4])
	if h.s != nil {
		t.Fatal("h.s != nil")
	}
	if h.m == nil {
		t.Fatal("h.m == nil")
	}
	if g := h.get("4"); g != nodes[4] {
		t.Fatal("4 diff")
	}
}
