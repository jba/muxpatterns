package muxpatterns

import (
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/exp/slices"
)

func TestParse(t *testing.T) {
	lit := func(name string) element {
		return element{s: name}
	}

	wild := func(name string) element {
		return element{s: name, wild: true}
	}

	multi := func(name string) element {
		s := wild(name)
		s.multi = true
		return s
	}

	for _, test := range []struct {
		in   string
		want Pattern
	}{
		{"/", Pattern{elements: []element{lit("/"), multi("")}}},
		{"/a", Pattern{elements: []element{lit("/a")}}},
		{
			"/a/",
			Pattern{elements: []element{lit("/a/"), multi("")}},
		},
		{"/path/to/something", Pattern{elements: []element{
			lit("/path/to/something"),
		}}},
		{
			"/{w1}/lit/{w2}",
			Pattern{
				elements: []element{lit("/"), wild("w1"), lit("/lit/"), wild("w2")},
			},
		},
		{
			"/{w1}/lit/{w2}/",
			Pattern{
				elements: []element{lit("/"), wild("w1"), lit("/lit/"), wild("w2"), lit("/"), multi("")},
			},
		},
		{
			"example.com/",
			Pattern{host: "example.com", elements: []element{lit("/"), multi("")}},
		},
		{
			"GET /",
			Pattern{method: "GET", elements: []element{lit("/"), multi("")}},
		},
		{
			"POST example.com/foo/{w}",
			Pattern{
				method:   "POST",
				host:     "example.com",
				elements: []element{lit("/foo/"), wild("w")},
			},
		},
		{
			"/{$}",
			Pattern{elements: []element{lit("/")}},
		},
		{
			"DELETE example.com/{$}",
			Pattern{method: "DELETE", host: "example.com", elements: []element{lit("/")}},
		},
		{
			"/foo/{$}",
			Pattern{elements: []element{lit("/foo/")}},
		},
		{
			"/{a}/foo/{rest...}",
			Pattern{elements: []element{lit("/"), wild("a"), lit("/foo/"), multi("rest")}},
		},
	} {
		got, err := Parse(test.in)
		if err != nil {
			t.Fatalf("%q: %v", test.in, err)
		}
		if !got.equal(&test.want) {
			t.Errorf("%q:\ngot  %#v\nwant %#v", test.in, got, &test.want)
		}
	}
}

func TestParseError(t *testing.T) {
	for _, test := range []struct {
		in       string
		contains string
	}{
		{"", "empty pattern"},
		{"MOOSE /", "bad method"},
		{" ", "missing /"},
		{"//", "empty path segment"},
		{"GET a.com/foo//", "empty path segment"},
		{"/{w}x", "bad wildcard segment"},
		{"/x{w}", "bad wildcard segment"},
		{"/{wx", "bad wildcard segment"},
		{"/{a$}", "bad wildcard name"},
		{"/{}", "empty wildcard"},
		{"/{...}", "bad wildcard name"},
		{"/{$...}", "bad wildcard"},
		{"/{$}/", "{$} not at end"},
		{"/{$}/x", "{$} not at end"},
		{"/{a...}/", "not at end"},
		{"/{a...}/x", "not at end"},
		{"{a}/b", "missing initial '/'"},
		{"/a/{x}/b/{x...}", "duplicate wildcard name"},
	} {
		_, err := Parse(test.in)
		if err == nil || !strings.Contains(err.Error(), test.contains) {
			t.Errorf("%q:\ngot %v, want error containing %q", test.in, err, test.contains)
		}
	}
}

func (p1 *Pattern) equal(p2 *Pattern) bool {
	return p1.method == p2.method && p1.host == p2.host && slices.Equal(p1.elements, p2.elements)
}

func TestMatch(t *testing.T) {
	for _, test := range []struct {
		method      string
		host        string
		path        string
		pattern     string
		wantMatch   bool
		wantMatches []string
	}{
		{
			path:      "/",
			pattern:   "/",
			wantMatch: true,
		},
		{
			method:    "GET",
			path:      "/",
			pattern:   "GET /",
			wantMatch: true,
		},
		{
			host:      "example.com",
			path:      "/",
			pattern:   "example.com/",
			wantMatch: true,
		},
		{
			method:    "TRACE",
			host:      "example.com",
			path:      "/",
			pattern:   "TRACE example.com/",
			wantMatch: true,
		},
		{
			path:      "/foo/bar/baz",
			pattern:   "/foo/bar/baz",
			wantMatch: true,
		},
		{
			path:      "/foo/bar/baz",
			pattern:   "/foo/bar",
			wantMatch: false,
		},
		{
			path:      "/foo/bar",
			pattern:   "/foo/bar/baz",
			wantMatch: false,
		},
		{
			// final slash is a like "{...}"
			path:      "/foo/",
			pattern:   "/foo/",
			wantMatch: true,
		},
		{
			path:      "/foo/bar/baz",
			pattern:   "/foo/",
			wantMatch: true,
		},
		{
			path:        "/foo/bar/baz",
			pattern:     "/{x}/",
			wantMatch:   true,
			wantMatches: []string{"foo"},
		},
		{
			path:        "/foo/bar/baz/qux",
			pattern:     "/foo/{a}/baz/{b}",
			wantMatch:   true,
			wantMatches: []string{"bar", "qux"},
		},
		{
			pattern:     "/{x...}",
			path:        "/",
			wantMatch:   true,
			wantMatches: []string{""},
		},
		{
			pattern:     "/{x...}",
			path:        "/a",
			wantMatch:   true,
			wantMatches: []string{"a"},
		},
		{
			pattern:     "/{x...}",
			path:        "/a/",
			wantMatch:   true,
			wantMatches: []string{"a/"},
		},
		{
			pattern:     "/{x...}",
			path:        "/a/b",
			wantMatch:   true,
			wantMatches: []string{"a/b"},
		},
		{
			path:        "/foo/bar/baz/qux",
			pattern:     "/foo/{a}/{b...}",
			wantMatch:   true,
			wantMatches: []string{"bar", "baz/qux"},
		},
		{
			path:        "/foo/bar/17/qux/moo",
			pattern:     "/foo/{a}/{n}/{b...}",
			wantMatch:   true,
			wantMatches: []string{"bar", "17", "qux/moo"},
		},
		{
			// "..."  can match nothing
			path:        "/foo/bar/17/",
			pattern:     "/foo/{a}/{n}/{b...}",
			wantMatch:   true,
			wantMatches: []string{"bar", "17", ""},
		},
		{
			path:      "/foo/bar/",
			pattern:   "/foo/bar/{$}",
			wantMatch: true,
		},
		{
			path:      "/a",
			pattern:   "/{$}",
			wantMatch: false,
		},
		{
			path:      "/a/",
			pattern:   "/a",
			wantMatch: false,
		},
		{
			path:      "/a/",
			pattern:   "/a/{x}",
			wantMatch: false,
		},
	} {
		pat, err := Parse(test.pattern)
		if err != nil {
			t.Fatal(err)
		}
		gotMatch, gotMatches := pat.Match(test.method, test.host, test.path)
		if g, w := gotMatch, test.wantMatch; g != w {
			t.Errorf("%q.Match(%q, %q, %q): got %t, want %t", pat, test.method, test.host, test.path, g, w)
			return
		}
		if g, w := gotMatches, test.wantMatches; !reflect.DeepEqual(g, w) {
			t.Errorf("matches: got %#v, want %#v", g, w)
		}
	}
}

func TestComparePaths(t *testing.T) {
	for _, test := range []struct {
		p1, p2 string
		want   string
	}{
		// TODO: verify we hit all these case below in our systematic list.
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
		{"/a/{x...}", "/b/{y...}", disjoint},
		{"/a/{x...}", "/a/{y...}", overlaps},
		{"/a/{z}/{x...}", "/a/b/{y...}", moreGeneral},
		{"/a/{z}/{x...}", "/{z}/b/{y...}", overlaps},
		{"/a/{x...}", "/a/{x}/{y...}", moreGeneral},

		// A non-final pattern segment can have one of two values: literal or
		// single wildcard. A final pattern segment can have one of 5: empty
		// (trailing slash), literal, dollar, single wildcard, or multi
		// wildcard. Trailing slash and multi wildcard are the same.

		// A literal should be more specific than anything it overlaps, except itself.
		{"/a", "/a", overlaps},
		{"/a", "/b", disjoint},
		{"/a", "/", moreSpecific},
		{"/a", "/{$}", disjoint},
		{"/a", "/{x}", moreSpecific},
		{"/a", "/{x...}", moreSpecific},

		// Adding a segment doesn't change that.
		{"/b/a", "/b/a", overlaps},
		{"/b/a", "/b/b", disjoint},
		{"/b/a", "/b/", moreSpecific},
		{"/b/a", "/b/{$}", disjoint},
		{"/b/a", "/b/{x}", moreSpecific},
		{"/b/a", "/b/{x...}", moreSpecific},
		{"/{z}/a", "/{z}/a", overlaps},
		{"/{z}/a", "/{z}/b", disjoint},
		{"/{z}/a", "/{z}/", moreSpecific},
		{"/{z}/a", "/{z}/{$}", disjoint},
		{"/{z}/a", "/{z}/{x}", moreSpecific},
		{"/{z}/a", "/{z}/{x...}", moreSpecific},

		// Single wildcard on left.
		{"/{z}", "/a", moreGeneral},
		{"/{z}", "/a/b", disjoint},
		{"/{z}", "/{$}", disjoint},
		{"/{z}", "/{x}", overlaps},
		{"/{z}", "/", moreSpecific},
		{"/{z}", "/{x...}", moreSpecific},
		{"/b/{z}", "/b/a", moreGeneral},
		{"/b/{z}", "/b/a/b", disjoint},
		{"/b/{z}", "/b/{$}", disjoint},
		{"/b/{z}", "/b/{x}", overlaps},
		{"/b/{z}", "/b/", moreSpecific},
		{"/b/{z}", "/b/{x...}", moreSpecific},

		// Trailing slash on left.
		{"/", "/a", moreGeneral},
		{"/", "/a/b", moreGeneral},
		{"/", "/{$}", moreGeneral},
		{"/", "/{x}", moreGeneral},
		{"/", "/", overlaps},
		{"/", "/{x...}", overlaps},

		{"/b/", "/b/a", moreGeneral},
		{"/b/", "/b/a/b", moreGeneral},
		{"/b/", "/b/{$}", moreGeneral},
		{"/b/", "/b/{x}", moreGeneral},
		{"/b/", "/b/", overlaps},
		{"/b/", "/b/{x...}", overlaps},

		{"/{z}/", "/{z}/a", moreGeneral},
		{"/{z}/", "/{z}/a/b", moreGeneral},
		{"/{z}/", "/{z}/{$}", moreGeneral},
		{"/{z}/", "/{z}/{x}", moreGeneral},
		{"/{z}/", "/{z}/", overlaps},
		{"/{z}/", "/a/", moreGeneral},
		{"/{z}/", "/{z}/{x...}", overlaps},
		{"/{z}/", "/a/{x...}", moreGeneral},
		{"/a/{z}/", "/{z}/a/", overlaps},

		// Multi wildcard on left.
		{"/{m...}", "/a", moreGeneral},
		{"/{m...}", "/a/b", moreGeneral},
		{"/{m...}", "/{$}", moreGeneral},
		{"/{m...}", "/{x}", moreGeneral},
		{"/{m...}", "/", overlaps},
		{"/{m...}", "/{x...}", overlaps},

		{"/b/{m...}", "/b/a", moreGeneral},
		{"/b/{m...}", "/b/a/b", moreGeneral},
		{"/b/{m...}", "/b/{$}", moreGeneral},
		{"/b/{m...}", "/b/{x}", moreGeneral},
		{"/b/{m...}", "/b/", overlaps},
		{"/b/{m...}", "/b/{x...}", overlaps},

		{"/{z}/{m...}", "/{z}/a", moreGeneral},
		{"/{z}/{m...}", "/{z}/a/b", moreGeneral},
		{"/{z}/{m...}", "/{z}/{$}", moreGeneral},
		{"/{z}/{m...}", "/{z}/{x}", moreGeneral},
		{"/{z}/{m...}", "/{z}/", overlaps},
		{"/{z}/{m...}", "/a/", moreGeneral},
		{"/{z}/{m...}", "/{z}/{x...}", overlaps},
		{"/{z}/{m...}", "/a/{x...}", moreGeneral},
		{"/a/{z}/{m...}", "/{z}/a/", overlaps},

		// Dollar on left.
		{"/{$}", "/a", disjoint},
		{"/{$}", "/a/b", disjoint},
		{"/{$}", "/{$}", overlaps},
		{"/{$}", "/{x}", disjoint},
		{"/{$}", "/", moreSpecific},
		{"/{$}", "/{x...}", moreSpecific},

		{"/b/{$}", "/b/a", disjoint},
		{"/b/{$}", "/b/a/b", disjoint},
		{"/b/{$}", "/b/{$}", overlaps},
		{"/b/{$}", "/b/{x}", disjoint},
		{"/b/{$}", "/b/", moreSpecific},
		{"/b/{$}", "/b/{x...}", moreSpecific},

		{"/{z}/{$}", "/{z}/a", disjoint},
		{"/{z}/{$}", "/{z}/a/b", disjoint},
		{"/{z}/{$}", "/{z}/{$}", overlaps},
		{"/{z}/{$}", "/{z}/{x}", disjoint},
		{"/{z}/{$}", "/{z}/", moreSpecific},
		{"/{z}/{$}", "/a/", overlaps},
		{"/{z}/{$}", "/a/{x...}", overlaps},
		{"/{z}/{$}", "/{z}/{x...}", moreSpecific},
		{"/a/{z}/{$}", "/{z}/a/", overlaps},
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

func TestHigherPrecedence(t *testing.T) {
	for _, test := range []struct {
		p1, p2 string
		want   bool
	}{
		// 1. host
		{"h/", "/", true},
		{"/", "h/", false},
		{"h/", "h/", false},

		// 2. method
		{"GET /", "/", true},
		{"/", "GET /", false},
		{"GET /", "POST /", false},

		// 3. more specific path
		{"/", "/", false},
		{"/a", "/", true},
		{"/", "/a", false},
		{"/a", "/a", false},
		{"/a/", "/a", false},
		{"/a", "/a/", false},
		{"/a", "/a/{x}", false},
		{"/a/{x}", "/a", false},
		{"/a/{x}", "/a/{x}", false},
		{"/a/{x...}", "/a/{x}", false},
		{"/a/{x}", "/a/{x...}", true},
		{"/a/bc", "/a/b", false},
		{"/a/b", "/a/bc", false},

		// 4. {$}
		{"/{$}", "/", true},
		{"/", "/{$}", false},
		{"/a/{x}/{$}", "/a/{x}/", true},
		{"/a/{x}/", "/a/{x}/{$}", false},
		{"/a/b/", "/a/{x}/{$}", false}, // old rule 3 would say it's true
		{"/a/{x}/{$}", "/a/b/", false},
		{"/a/{$}", "/b/{$}", false},

		// false
		{"/{x}/{y}", "/{x}/a", false},
	} {
		pat1, err := Parse(test.p1)
		if err != nil {
			t.Fatal(err)
		}
		pat2, err := Parse(test.p2)
		if err != nil {
			t.Fatal(err)
		}
		got := pat1.HigherPrecedence(pat2)
		if got != test.want {
			t.Errorf("%q.HigherPrecedence(%q) = %t, want %t",
				test.p1, test.p2, got, test.want)
		}
	}
}

func TestConflictsWith(t *testing.T) {
	for _, test := range []struct {
		p1, p2 string
		want   bool
	}{
		{"/a", "/a", true},
		{"/a", "/ab", false},
		{"/a/b/cd", "/a/b/cd", true},
		{"/a/b/cd", "/a/b/c", false},
		{"/a/b/c", "/a/c/c", false},
		{"/{x}", "/{y}", true},
		{"/{x}", "/a", false}, // more specific
		{"/{x}/{y}", "/{x}/a", false},
		{"/{x}/{y}", "/{x}/a/b", false},
		{"/{x}", "/a/{y}", false},
		{"/{x}/{y}", "/{x}/a/", false},
		{"/{x}", "/a/{y...}", false},           // more specific
		{"/{x}/a/{y}", "/{x}/a/{y...}", false}, // more specific
		{"/{x}/{y}", "/{x}/a/{$}", false},      // more specific
		{"/{x}/{y}/{$}", "/{x}/a/{$}", false},
		{"/a/{x}", "/{x}/b", true},
	} {
		pat1, err := Parse(test.p1)
		if err != nil {
			t.Fatal(err)
		}
		pat2, err := Parse(test.p2)
		if err != nil {
			t.Fatal(err)
		}
		got := pat1.ConflictsWith(pat2)
		if got != test.want {
			t.Errorf("%q.ConflictsWith(%q) = %t, want %t",
				test.p1, test.p2, got, test.want)
		}
		// ConflictsWith should be commutative.
		got = pat2.ConflictsWith(pat1)
		if got != test.want {
			t.Errorf("%q.ConflictsWith(%q) = %t, want %t",
				test.p2, test.p1, got, test.want)
		}
	}
}

func TestPatternSetMatch(t *testing.T) {
	var ps PatternSet
	for _, p := range []string{
		"/item/",
		"POST /item/{user}",
		"/item/{user}",
		"/item/{user}/{id}",
		"/item/{user}/new",
		"/item/{$}",
		"POST alt.com/item/{userp}",
		"/path/{p...}",
	} {
		pat, err := Parse(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := ps.Register(pat); err != nil {
			t.Fatal(err)
		}
	}

	for _, test := range []struct {
		method, host, path string
		want               map[string]string // nil -> no match, empty -> match
	}{
		{"GET", "", "/item/jba",
			map[string]string{"user": "jba"}},
		{"POST", "", "/item/jba/17",
			map[string]string{"user": "jba", "id": "17"}},
		{"GET", "", "/item/jba/new",
			map[string]string{"user": "jba"}},
		{"GET", "", "/item/",
			map[string]string{}}, // matches with no bindings
		{"GET", "", "/item/jba/17/line2",
			map[string]string{}}, // matches with no bindings
		{"POST", "alt.com", "/item/jba",
			map[string]string{"userp": "jba"}},
		{"GET", "alt.com", "/item/jba",
			map[string]string{"user": "jba"}},
		{"GET", "", "/item", nil}, // does not match
		{"GET", "", "/path/to/file",
			map[string]string{"p": "to/file"}},
	} {
		if test.host == "" {
			test.host = "example.com"
		}
		t.Run(fmt.Sprintf("%s,%s,%s", test.method, test.host, test.path), func(t *testing.T) {
			p, got := ps.Match(test.method, test.host, test.path)
			if p == nil {
				if test.want != nil {
					t.Error("got no match, wanted match")
				}
				return
			}
			if !maps.Equal(got, test.want) {
				t.Errorf("got %v\nwant %v", got, test.want)
			}
		})
	}
}
