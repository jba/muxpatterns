// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	"golang.org/x/exp/maps"
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
		root.addPattern(pat, nil)
	}
	return root
}

func TestAddPattern(t *testing.T) {
	want := `"":
    "":
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
            "":
                "j":
                    "/g/{x}/j"
            "h":
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
	wantPat            string // "" for nil (no match)
	wantMatches        []string
}

func TestNodeMatch(t *testing.T) {

	test := func(tree *node, tests []testCase) {
		t.Helper()
		for _, test := range tests {
			gotNode, gotMatches := tree.match(test.method, test.host, test.path)
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

	tree := buildTree(
		"/item/",
		"POST /item/{user}",
		"GET /item/{user}",
		"/item/{user}",
		"/item/{user}/{id}",
		"/item/{user}/new",
		"/item/{$}",
		"POST alt.com/item/{user}",
		"GET /headwins",
		"HEAD /headwins",
		"/path/{p...}")

	test(tree, []testCase{
		{"GET", "", "/item/jba",
			"GET /item/{user}", []string{"jba"}},
		{"POST", "", "/item/jba",
			"POST /item/{user}", []string{"jba"}},
		{"HEAD", "", "/item/jba",
			"GET /item/{user}", []string{"jba"}},
		{"get", "", "/item/jba",
			"/item/{user}", []string{"jba"}}, // method matches are case-sensitive
		{"POST", "", "/item/jba/17",
			"/item/{user}/{id}", []string{"jba", "17"}},
		{"GET", "", "/item/jba/new",
			"/item/{user}/new", []string{"jba"}},
		{"GET", "", "/item/",
			"/item/{$}", []string{}},
		{"GET", "", "/item/jba/17/line2",
			"/item/", nil},
		{"POST", "alt.com", "/item/jba",
			"POST alt.com/item/{user}", []string{"jba"}},
		{"GET", "alt.com", "/item/jba",
			"GET /item/{user}", []string{"jba"}},
		{"GET", "", "/item",
			"", nil}, // does not match
		{"GET", "", "/headwins",
			"GET /headwins", nil},
		{"HEAD", "", "/headwins", // HEAD is more specific than GET
			"HEAD /headwins", nil},
		{"GET", "", "/path/to/file",
			"/path/{p...}", []string{"to/file"}},
	})

	// A pattern ending in {$} should only match URLS with a trailing slash.
	pat1 := "/a/b/{$}"
	test(buildTree(pat1), []testCase{
		{"GET", "", "/a/b", "", nil},
		{"GET", "", "/a/b/", pat1, nil},
		{"GET", "", "/a/b/c", "", nil},
		{"GET", "", "/a/b/c/d", "", nil},
	})

	// A pattern ending in a single wildcard should not match a trailing slash URL.
	pat2 := "/a/b/{w}"
	test(buildTree(pat2), []testCase{
		{"GET", "", "/a/b", "", nil},
		{"GET", "", "/a/b/", "", nil},
		{"GET", "", "/a/b/c", pat2, []string{"c"}},
		{"GET", "", "/a/b/c/d", "", nil},
	})

	// A pattern ending in a multi wildcard should match both URLs.
	pat3 := "/a/b/{w...}"
	test(buildTree(pat3), []testCase{
		{"GET", "", "/a/b", "", nil},
		{"GET", "", "/a/b/", pat3, []string{""}},
		{"GET", "", "/a/b/c", pat3, []string{"c"}},
		{"GET", "", "/a/b/c/d", pat3, []string{"c/d"}},
	})

	// All three of the above should work together.
	test(buildTree(pat1, pat2, pat3), []testCase{
		{"GET", "", "/a/b", "", nil},
		{"GET", "", "/a/b/", pat1, nil},
		{"GET", "", "/a/b/c", pat2, []string{"c"}},
		{"GET", "", "/a/b/c/d", pat3, []string{"c/d"}},
	})
}

func TestMatchingMethods(t *testing.T) {
	hostTree := buildTree("GET a.com/", "PUT b.com/", "POST /foo/{x}")
	for _, test := range []struct {
		name       string
		tree       *node
		host, path string
		want       string
	}{
		{
			"post",
			buildTree("POST /"), "", "/foo",
			"POST",
		},
		{
			"get",
			buildTree("GET /"), "", "/foo",
			"GET,HEAD",
		},
		{
			"host",
			hostTree, "", "/foo",
			"",
		},
		{
			"host",
			hostTree, "", "/foo/bar",
			"POST",
		},
		{
			"host2",
			hostTree, "a.com", "/foo/bar",
			"GET,HEAD,POST",
		},
		{
			"host3",
			hostTree, "b.com", "/bar",
			"PUT",
		},
		{
			// This case shouldn't come up because we only call matchingMethods
			// when there was no match, but we include it for completeness.
			"empty",
			buildTree("/"), "", "/",
			"",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ms := map[string]bool{}
			test.tree.matchingMethods(test.host, test.path, ms)
			keys := maps.Keys(ms)
			sort.Strings(keys)
			got := strings.Join(keys, ",")
			if got != test.want {
				t.Errorf("got %s, want %s", got, test.want)
			}
		})
	}
}

func (n *node) print(w io.Writer, level int) {
	indent := strings.Repeat("    ", level)
	if n.pattern != nil {
		fmt.Fprintf(w, "%s%q\n", indent, n.pattern)
	}
	if n.emptyChild != nil {
		fmt.Fprintf(w, "%s%q:\n", indent, "")
		n.emptyChild.print(w, level+1)
	}

	var keys []string
	n.children.pairs(func(k string, _ *node) bool {
		keys = append(keys, k)
		return true
	})
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(w, "%s%q:\n", indent, k)
		n, _ := n.children.find(k)
		n.print(w, level+1)
	}
}
