// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package muxpatterns

import (
	"fmt"
	"strconv"
	"testing"
)

func TestMapping(t *testing.T) {
	var nodes []*node
	for i := 0; i < maxSlice; i++ {
		nodes = append(nodes, &node{})
	}
	var h mapping[string, *node]
	for i := 0; i < maxSlice; i++ {
		h.add(strconv.Itoa(i), nodes[i])
	}
	if h.m != nil {
		t.Fatal("h.m != nil")
	}
	for i := 0; i < maxSlice; i++ {
		g, _ := h.find(strconv.Itoa(i))
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
	if g, _ := h.find("4"); g != nodes[4] {
		t.Fatal("4 diff")
	}
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
				var entries []entry[string, *node]
				for _, c := range list {
					entries = append(entries, entry[string, *node]{c, nil})
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
				var h mapping[string, *node]
				for _, c := range list {
					h.add(c, nil)
				}
				var x *node
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					x, _ = h.find(key)
				}
				_ = x
			})
		})
	}
}

func findChildLinear(key string, entries []entry[string, *node]) *node {
	for _, e := range entries {
		if key == e.key {
			return e.value
		}
	}
	return nil
}
