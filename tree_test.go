// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"strings"
	"testing"

	"golang.org/x/exp/slices"
)

func TestToSegments(t *testing.T) {
	for _, test := range []struct {
		in   string
		want []segment
	}{
		{"/", []segment{{wild: true, multi: true}}},
		{"/a/bc/d", []segment{
			{s: "a"},
			{s: "bc"},
			{s: "d"},
		}},
		{"/a/{x}/b", []segment{
			{s: "a"},
			{wild: true},
			{s: "b"},
		}},
		{"/a/{x}/b/{$}", []segment{
			{s: "a"},
			{wild: true},
			{s: "b"},
			{s: "/"},
		}},
		{"/a/{x}/b/{z...}", []segment{
			{s: "a"},
			{wild: true},
			{s: "b"},
			{wild: true, multi: true},
		}},
		{"/a/{x}/b/", []segment{
			{s: "a"},
			{wild: true},
			{s: "b"},
			{wild: true, multi: true},
		}},
	} {
		p, err := Parse(test.in)
		if err != nil {
			t.Fatal(err)
		}
		got := p.toSegments()
		if !slices.Equal(got, test.want) {
			t.Errorf("%s:\ngot  %v\nwant %v", test.in, got, test.want)
		}
	}
}

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

func init() {
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
                    "/a/b/"
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
	testTree.print(&b, 0)
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
		{"/a/b/", "/a/b/", nil},
		{"/a/b/c", "/a/b/{y}", []string{"c"}},
		{"/a/b/c/d", "/a/b/{x...}", []string{"c/d"}},
		{"/g/h/i", "/g/h/i", nil},
		{"/g/h/j", "/g/{x}/j", []string{"h"}},
	} {
		gotPat, gotMatches := testTree.match("", "", test.path)
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
