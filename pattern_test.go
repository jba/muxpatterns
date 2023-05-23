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
	lit := func(name string) segment {
		return segment{s: name}
	}

	wild := func(name string) segment {
		return segment{s: name, wild: true}
	}

	multi := func(name string) segment {
		s := wild(name)
		s.multi = true
		return s
	}

	for _, test := range []struct {
		in   string
		want Pattern
	}{
		{"/", Pattern{segments: []segment{multi("")}}},
		{
			"/a/",
			Pattern{segments: []segment{lit("a"), multi("")}},
		},
		{"/path/to/something", Pattern{segments: []segment{
			lit("path"),
			lit("to"),
			lit("something"),
		}}},
		{
			"/{w1}/lit/{w2}",
			Pattern{
				segments: []segment{wild("w1"), lit("lit"), wild("w2")},
			},
		},
		{
			"/{w1}/lit/{w2}/",
			Pattern{
				segments: []segment{wild("w1"), lit("lit"), wild("w2"), multi("")},
			},
		},
		{
			"example.com/",
			Pattern{host: "example.com", segments: []segment{multi("")}},
		},
		{
			"GET /",
			Pattern{method: "GET", segments: []segment{multi("")}},
		},
		{
			"POST example.com/foo/{w}",
			Pattern{
				method:   "POST",
				host:     "example.com",
				segments: []segment{lit("foo"), wild("w")},
			},
		},
		{
			"/{$}",
			Pattern{segments: []segment{wild("$")}},
		},
		{
			"DELETE example.com/{$}",
			Pattern{method: "DELETE", host: "example.com", segments: []segment{wild("$")}},
		},
		{
			"/foo/{$}",
			Pattern{segments: []segment{lit("foo"), wild("$")}},
		},
		{
			"/{a}/foo/{rest...}",
			Pattern{segments: []segment{wild("a"), lit("foo"), multi("rest")}},
		},
	} {
		got, err := Parse(test.in)
		if err != nil {
			t.Fatalf("%q: %v", test.in, err)
		}
		if !got.equal(&test.want) {
			t.Errorf("%q:\ngot  %s\nwant %s", test.in, got, &test.want)
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
		{"/{}", "bad wildcard name"},
		{"/{...}", "bad wildcard name"},
		{"/{$...}", "bad wildcard"},
		{"/{$}/", "must be at end"},
		{"/{$}/x", "must be at end"},
		{"/{a...}/", "must be at end"},
		{"/{a...}/x", "must be at end"},
	} {
		_, err := Parse(test.in)
		if err == nil || !strings.Contains(err.Error(), test.contains) {
			t.Errorf("%q:\ngot %v, want error containing %q", test.in, err, test.contains)
		}
	}
}

func (p1 *Pattern) equal(p2 *Pattern) bool {
	return p1.method == p2.method && p1.host == p2.host && slices.Equal(p1.segments, p2.segments)
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
	} {
		t.Run(fmt.Sprintf("%s,%s,%s", test.method, test.host, test.path), func(t *testing.T) {
			pat, err := Parse(test.pattern)
			if err != nil {
				t.Fatal(err)
			}
			gotMatch, gotMatches := pat.Match(test.method, test.host, test.path)
			if g, w := gotMatch, test.wantMatch; g != w {
				t.Errorf("match: got %t, want %t", g, w)
			}
			if g, w := gotMatches, test.wantMatches; !reflect.DeepEqual(g, w) {
				t.Errorf("matches: got %#v, want %#v", g, w)
			}
		})
	}
}

func TestLiteralPrefixLen(t *testing.T) {
	for _, test := range []struct {
		pattern string
		want    int
	}{
		{"/", 1},
		{"/{x}", 1},
		{"/{$}", 1},
		{"/a", 2},
		{"/abc", 4},
		{"/a/bc", 5},
		{"/a/bc/{x}", 6},
		{"/a/b/{$}", 5},
		{"/{x}/{y}", 1},
		{"/{x}/a", 1},
	} {
		p, err := Parse(test.pattern)
		if err != nil {
			t.Fatal(err)
		}
		got := p.literalPrefixLen()
		if got != test.want {
			t.Errorf("%q: got %d, want %d", test.pattern, got, test.want)
		}
	}
}

func TestMoreSpecificThan(t *testing.T) {
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

		// 3. literal prefix
		{"/", "/", false},
		{"/a", "/", true},
		{"/", "/a", false},
		{"/a", "/a", false},
		{"/a/", "/a", true},
		{"/a", "/a/", false},
		{"/a", "/a/{x}", false},
		{"/a/{x}", "/a", true},
		{"/a/{x}", "/a/{x}", false},
		{"/a/{x...}", "/a/{x}", false},
		{"/a/bc", "/a/b", true},
		{"/a/b", "/a/bc", false},

		// 4. {$}
		{"/{$}", "/", true},
		{"/", "/$", false},
		{"/a/{x}/{$}", "/a/{x}/", true},
		{"/a/{x}/", "/a/{x}/{$}", false},
		{"/a/b/", "/a/{x}/{$}", true},
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
		got := pat1.MoreSpecificThan(pat2)
		if got != test.want {
			t.Errorf("%q.MoreSpecificThan(%q) = %t, want %t",
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
		{"/{x}/{y}", "/{x}/a", true},
		{"/{x}/{y}", "/{x}/a/b", false},
		{"/{x}", "/a/{y}", false}, // more specific
		{"/{x}/{y}", "/{x}/a/", false},
		{"/{x}", "/a/{y...}", false},           // more specific
		{"/{x}/a/{y}", "/{x}/a/{y...}", false}, // more specific
		{"/{x}/{y}", "/{x}/a/{$}", false},      // more specific
		{"/{x}/{y}/{$}", "/{x}/a/{$}", true},
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
		"/item/{$}",
		"POST alt.com/item/{userp}",
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
		want               map[string]any // nil -> no match, empty -> match
	}{
		{"GET", "", "/item/jba", map[string]any{"user": "jba"}},
		{"POST", "", "/item/jba/17", map[string]any{"user": "jba", "id": "17"}},
		{"GET", "", "/item/", map[string]any{}},
		{"GET", "", "/item/jba/17/line2", map[string]any{}},
		{"POST", "alt.com", "/item/jba", map[string]any{"userp": "jba"}},
		{"GET", "alt.com", "/item/jba", map[string]any{"user": "jba"}},
		{"GET", "", "/item", nil},
	} {
		if test.host == "" {
			test.host = "example.com"
		}
		t.Run(fmt.Sprintf("%s,%s,%s", test.method, test.host, test.path), func(t *testing.T) {
			p, got, err := ps.Match(test.method, test.host, test.path)
			if err != nil {
				t.Fatal(err)
			}
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
