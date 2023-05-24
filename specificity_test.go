// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"testing"
)

func TestSpecif(t *testing.T) {
	for _, test := range []struct {
		p1, p2 string
		want   string
	}{
		{"/a", "/a", overlaps},
		{"/a", "/ab", disjoint},
		{"/{x}", "/{y}", overlaps},
		{"/{x}", "/a", moreGeneral},
		{"/{x}/b", "/a/{y}", overlaps},
		{"/{$}", "/a", disjoint},
		{"/a/{$}", "/a", disjoint},
		{"/", "/a", moreGeneral},
		{"/{x...}", "/a", moreGeneral},
		{"/", "/{x}", moreGeneral},
		{"/", "/{$}", moreGeneral},
		{"/a/b/{x...}", "/a/b/c/d/{y...}", moreGeneral},
		{"/a", "/a", overlaps},
		{"/a", "/ab", disjoint},
		{"/{x}", "/{x}", overlaps},
		{"/a/{x...}", "/a/b/{x...}", moreGeneral},
		{"/a/{$}", "/a/b/{x...}", disjoint},
		{"/a/b/{$}", "/a/b/{x...}", moreSpecific},
		{"/a/{x}/b/{y...}", "/{x}/c/{y...}", overlaps},
		{"/a/{x}/b/", "/{x}/c/{y...}", overlaps},
		{"/a/{x}/b/{$}", "/{x}/c/{y...}", overlaps},
	} {
		pat1, err := Parse(test.p1)
		if err != nil {
			t.Fatal(err)
		}
		pat2, err := Parse(test.p2)
		if err != nil {
			t.Fatal(err)
		}
		if g := pat1.comparePaths(pat1); g != overlaps {
			t.Errorf("%s does not match itself; got %s", pat1, g)
		}
		if g := pat2.comparePaths(pat2); g != overlaps {
			t.Errorf("%s does not match itself; got %s", pat2, g)
		}
		got := pat1.comparePaths(pat2)
		if got != test.want {
			t.Errorf("%s vs %s: got %s, want %s", test.p1, test.p2, got, test.want)
		}
		var want2 string
		switch test.want {
		case moreSpecific:
			want2 = moreGeneral
		case moreGeneral:
			want2 = moreSpecific
		default:
			want2 = test.want
		}
		got2 := pat2.comparePaths(pat1)
		if got2 != want2 {
			t.Errorf("%s vs %s: got %s, want %s", test.p2, test.p1, got2, want2)
		}
	}
}

/*
func TestOverlap(t *testing.T) {
	for _, test := range []struct {
		p1, p2 string
		want   string
	}{
		{"/a", "/a", "/a"},
		{"/a", "/ab", ""},
		{"/{x}", "/{x}", "/x"},
		{"/{x}", "/a", "/a"},
		{"/{x}/b", "/a/{y}", "/a/b"},
		{"/{$}", "/a", ""},
		{"/a/{$}", "/a", ""},
		{"/", "/a", "/a"},
		{"/{x...}", "/a", "/a"},
		{"/", "/{x}", "/x"},
		{"/", "/{$}", "/"},
		{"/a/{x...}", "/a/b/{x...}", "/a/b/"},
		{"/a/{$}", "/a/b/{x...}", ""},
		{"/a/b/{$}", "/a/b/{x...}", "/a/b/"},
		{"/a/b/{x...}", "/a/b/{$}", "/a/b/"},
		{"/a/{x}/b/{y...}", "/{x}/c/{y...}", "/a/c/b/"},
		{"/a/{x}/b/", "/{x}/c/{y...}", "/a/c/b/"},
		{"/a/{x}/b/{$}", "/{x}/c/{y...}", "/a/c/b/"},
	} {
		pat1, err := Parse(test.p1)
		if err != nil {
			t.Fatal(err)
		}
		pat2, err := Parse(test.p2)
		if err != nil {
			t.Fatal(err)
		}
		got := pat1.overlap(pat2)
		if got != test.want {
			t.Errorf("%s vs. %s: got %q, want %q", test.p1, test.p2, got, test.want)
		}

		got2 := pat2.overlap(pat1)
		if got2 != got {
			t.Errorf("%s vs %s: reverse differed: %q vs. %q", test.p2, test.p1, got, got2)
		}
	}
}
*/
