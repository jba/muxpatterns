// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"

	"golang.org/x/exp/slices"
)

// This test works by comparing possiblyConflictingPatterns with
// an exhaustive loop through all patterns.
func FuzzIndex(f *testing.F) {
	inits := []string{"/a", "/a/b", "/{x0}", "/{x0}/b", "/a/{x0}", "/a/{$}", "/a/b/{$}",
		"/a/", "/a/b/", "/{x}/b/c/{$}", "GET /{x0}/"}

	var patterns []*Pattern
	idx := newIndex()

	compare := func(pat *Pattern) {
		got := indexConflicts(pat, idx)
		want := trueConflicts(pat, patterns)
		if !slices.Equal(got, want) {
			f.Fatalf("%q:\ngot  %s\nwant %s", pat, got, want)
		}
	}

	for _, p := range inits {
		pat, err := Parse(p)
		if err != nil {
			f.Fatal(err)
		}
		compare(pat)
		patterns = append(patterns, pat)
		idx.addPattern(pat)
		f.Add(bytesFromPattern(pat))
	}

	f.Fuzz(func(t *testing.T, pb []byte) {
		pat := bytesToPattern(pb)
		if pat == nil {
			return
		}
		compare(pat)
	})
}

func trueConflicts(pat *Pattern, pats []*Pattern) []string {
	var s []string
	for _, p := range pats {
		if pat.ConflictsWith(p) {
			s = append(s, p.String())
		}
	}
	sort.Strings(s)
	return s
}

func indexConflicts(pat *Pattern, idx *index) []string {
	var s []string
	idx.possiblyConflictingPatterns(pat, func(p *Pattern) error {
		if pat.ConflictsWith(p) {
			s = append(s, p.String())
		}
		return nil
	})
	sort.Strings(s)
	return slices.Compact(s)
}

// TODO: incorporate host and method; make encoding denser.
func bytesToPattern(bs []byte) *Pattern {
	if len(bs) == 0 {
		return nil
	}
	var sb strings.Builder
	wc := 0
	for _, b := range bs[:len(bs)-1] {
		sb.WriteByte('/')
		switch b & 0x3 {
		case 0:
			fmt.Fprintf(&sb, "{x%d}", wc)
			wc++
		case 1:
			sb.WriteString("a")
		case 2:
			sb.WriteString("b")
		case 3:
			sb.WriteString("c")
		}
	}
	sb.WriteByte('/')
	switch bs[len(bs)-1] & 0x7 {
	case 0:
		fmt.Fprintf(&sb, "{x%d}", wc)
	case 1:
		sb.WriteString("a")
	case 2:
		sb.WriteString("b")
	case 3:
		sb.WriteString("c")
	case 4, 5:
		fmt.Fprintf(&sb, "{x%d...}", wc)
	default:
		sb.WriteString("{$}")
	}
	pat, err := Parse(sb.String())
	if err != nil {
		panic(err)
	}
	return pat
}

func bytesFromPattern(p *Pattern) []byte {
	var bs []byte
	for _, s := range p.segments {
		var b byte
		switch {
		case s.multi:
			b = 4
		case s.wild:
			b = 0
		case s.s == "/":
			b = 7
		case s.s == "a":
			b = 1
		case s.s == "b":
			b = 2
		case s.s == "c":
			b = 3
		default:
			panic("bad pattern")
		}
		bs = append(bs, b)
	}
	return bs
}

func TestBytesPattern(t *testing.T) {
	tests := []struct {
		bs  []byte
		pat string
	}{
		{[]byte{0, 1, 2, 3}, "/{x0}/a/b/c"},
		{[]byte{16, 17, 18, 19}, "/{x0}/a/b/c"},
		{[]byte{4, 4}, "/{x0}/{x1...}"},
		{[]byte{6, 7}, "/b/{$}"},
	}
	t.Run("To", func(t *testing.T) {
		for _, test := range tests {
			p := bytesToPattern(test.bs)
			got := p.String()
			if got != test.pat {
				t.Errorf("%v: got %q, want %q", test.bs, got, test.pat)
			}
		}
	})
	t.Run("From", func(t *testing.T) {
		for _, test := range tests {
			p, err := Parse(test.pat)
			if err != nil {
				t.Fatal(err)
			}
			got := bytesFromPattern(p)
			var want []byte
			for _, b := range test.bs[:len(test.bs)-1] {
				want = append(want, b%4)

			}
			want = append(want, test.bs[len(test.bs)-1]%8)
			if !bytes.Equal(got, want) {
				t.Errorf("%s: got %v, want %v", test.pat, got, want)
			}
		}
	})
}
