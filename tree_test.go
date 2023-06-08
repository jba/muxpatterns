// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"fmt"
	"io"
	"sort"
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
		testTree = buildTree("/a", "/a/b", "/a/{x}",
			"/g/h/i", "/g/{x}/j",
			"/a/b/{x...}", "/a/b/{y}", "/a/b/{$}")
	}
	return testTree
}

func buildTree(pats ...string) *node {
	root := &node{}
	for _, p := range pats {
		pat, err := Parse(p)
		if err != nil {
			panic(err)
		}
		root.addPattern(pat, nil, "")
	}
	return root
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

type testCase struct {
	method, host, path string
	wantPat            string // "" for nil
	wantMatches        []string
}

func TestNodeMatch(t *testing.T) {

	test := func(tree *node, tests []testCase) {
		for _, test := range tests {
			gotNode, gotMatches := tree.match("GET", "", test.path)
			got := ""
			if gotNode != nil {
				got = gotNode.pattern.String()
			}
			if got != test.wantPat {
				t.Errorf("%s, %s, %s: got %q, want %q", test.method, test.host, test.path, got, test.wantPat)
			}
			if !slices.Equal(gotMatches, test.wantMatches) {
				t.Errorf("%s, %s, %s: got matches %v, want %v", test.method, test.host, test.path, gotMatches, test.wantMatches)
			}
		}
	}

	test(getTestTree(), []testCase{
		{"GET", "", "/a", "/a", nil},
		{"Get", "", "/b", "", nil},
		{"Get", "", "/a/b", "/a/b", nil},
		{"Get", "", "/a/c", "/a/{x}", []string{"c"}},
		{"Get", "", "/a/b/", "/a/b/{$}", nil},
		{"Get", "", "/a/b/c", "/a/b/{y}", []string{"c"}},
		{"Get", "", "/a/b/c/d", "/a/b/{x...}", []string{"c/d"}},
		{"Get", "", "/g/h/i", "/g/h/i", nil},
		{"Get", "", "/g/h/j", "/g/{x}/j", []string{"h"}},
	})

	tree := buildTree("/item/",
		"POST /item/{user}",
		"/item/{user}",
		"/item/{user}/{id}",
		"/item/{user}/new",
		"/item/{$}",
		"POST alt.com/item/{userp}",
		"/path/{p...}")
	test(tree, []testCase{
		{"GET", "", "/item/jba",
			"/item/{user}", []string{"jba"}},
		// {"POST", "", "/item/jba/17", []string{"jba", "17"}},
		// {"GET", "", "/item/jba/new", []string{"jba"}},
		// {"GET", "", "/item/", []string{}},
		// {"GET", "", "/item/jba/17/line2",nil},
		// {"POST", "alt.com", "/item/jba", []string{"jba"}},
		// {"GET", "alt.com", "/item/jba", []string{"jba"}},
		// {"GET", "", "/item", nil}, // does not match
		// {"GET", "", "/path/to/file", []string{"to/file"}},
	})
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
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {

			b.Run("rep=linear", func(b *testing.B) {
				var entries []entry
				for _, c := range list {
					entries = append(entries, entry{c, nil})
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					findChildLinear(key, entries)
				}
			})
			b.Run("rep=map", func(b *testing.B) {
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
			b.Run(fmt.Sprintf("rep=hybrid%d", maxSlice), func(b *testing.B) {
				var h mapping
				for _, c := range list {
					h.add(c, nil)
				}
				var x *node
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					x = h.find(key)
				}
				_ = x
			})
		})
	}
}

func TestHybrid(t *testing.T) {
	var nodes []*node
	for i := 0; i < maxSlice; i++ {
		nodes = append(nodes, &node{})
	}
	var h mapping
	for i := 0; i < maxSlice; i++ {
		h.add(strconv.Itoa(i), nodes[i])
	}
	if h.m != nil {
		t.Fatal("h.m != nil")
	}
	for i := 0; i < maxSlice; i++ {
		g := h.find(strconv.Itoa(i))
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
	if g := h.find("4"); g != nodes[4] {
		t.Fatal("4 diff")
	}
}

// Modifies n; use for testing only.
func (n *node) print(w io.Writer, level int) {
	indent := strings.Repeat("    ", level)
	if n.pattern != nil {
		fmt.Fprintf(w, "%s%q\n", indent, n.pattern)
	} else {
		fmt.Fprintf(w, "%snil\n", indent)
	}
	if n.emptyChild != nil {
		fmt.Fprintf(w, "%s%q:\n", indent, "")
		n.emptyChild.print(w, level+1)
	}

	keys := n.children.keys()
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(w, "%s%q:\n", indent, k)
		n.children.find(k).print(w, level+1)
	}
}
