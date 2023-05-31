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

func TestAddPattern(t *testing.T) {
	root := &node{}
	for _, p := range []string{"/a/b", "/a/{x}", "/a", "/a/b/", "/a/b/{y}", "/a/b/{$}"} {
		pat, err := Parse(p)
		if err != nil {
			t.Fatal(err)
		}
		root.addPattern(pat)
	}
	want := `nil
"a":
    "/a"
    "":
        "/a/{x}"
    "b":
        "/a/b"
        "":
            "/a/b/{y}"
        "*":
            "/a/b/{...}"
        "/":
            "/a/b/"
`

	var b strings.Builder
	root.print(&b, 0)
	got := b.String()
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
