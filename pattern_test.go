package muxpatterns

import (
	"net/http"
	"regexp"
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
		{"/a", Pattern{segments: []segment{lit("a")}}},
		{
			"/a/",
			Pattern{segments: []segment{lit("a"), multi("")}},
		},
		{"/path/to/something", Pattern{segments: []segment{
			lit("path"), lit("to"), lit("something"),
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
			Pattern{segments: []segment{lit("/")}},
		},
		{
			"DELETE example.com/a/{foo12}/{$}",
			Pattern{method: "DELETE", host: "example.com", segments: []segment{lit("a"), wild("foo12"), lit("/")}},
		},
		{
			"/foo/{$}",
			Pattern{segments: []segment{lit("foo"), lit("/")}},
		},
		{
			"/{a}/foo/{rest...}",
			Pattern{segments: []segment{wild("a"), lit("foo"), multi("rest")}},
		},
		{
			"//",
			Pattern{segments: []segment{lit(""), multi("")}},
		},
		{
			"/foo///./../bar",
			Pattern{segments: []segment{lit("foo"), lit(""), lit(""), lit("."), lit(".."), lit("bar")}},
		},
		{
			"a.com/foo//",
			Pattern{host: "a.com", segments: []segment{lit("foo"), lit(""), multi("")}},
		},
	} {
		got := mustParse(t, test.in)
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
		{"/{w}x", "bad wildcard segment"},
		{"/x{w}", "bad wildcard segment"},
		{"/{wx", "bad wildcard segment"},
		{"/{a$}", "bad wildcard name"},
		{"/{}", "empty wildcard"},
		{"/{...}", "empty wildcard"},
		{"/{$...}", "bad wildcard"},
		{"/{$}/", "{$} not at end"},
		{"/{$}/x", "{$} not at end"},
		{"/{a...}/", "not at end"},
		{"/{a...}/x", "not at end"},
		{"{a}/b", "missing initial '/'"},
		{"/a/{x}/b/{x...}", "duplicate wildcard name"},
		{"GET //", "unclean path"},
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

func TestComparePaths(t *testing.T) {
	for _, test := range []struct {
		p1, p2 string
		want   relationship
	}{
		// TODO: verify we hit all these case below in our systematic list.
		{"/a/{$}", "/a", disjoint},
		{"/", "/a", moreGeneral},
		{"/{x...}", "/a", moreGeneral},
		{"/", "/{x}", moreGeneral},
		{"/", "/{$}", moreGeneral},
		{"/a/b/{x...}", "/a/b/c/d/{y...}", moreGeneral},
		{"/a/{x...}", "/a/b/{x...}", moreGeneral},
		{"/a/{$}", "/a/b/{x...}", disjoint},
		{"/a/b/{$}", "/a/b/{x...}", moreSpecific},
		{"/a/{x}/b/{y...}", "/{x}/c/{y...}", overlaps},
		{"/a/{x}/b/", "/{x}/c/{y...}", overlaps},
		{"/a/{x}/b/{$}", "/{x}/c/{y...}", overlaps},
		{"/a/{x...}", "/b/{y...}", disjoint},
		{"/a/{x...}", "/a/{y...}", equivalent},
		{"/a/{z}/{x...}", "/a/b/{y...}", moreGeneral},
		{"/a/{z}/{x...}", "/{z}/b/{y...}", overlaps},
		{"/a/{x...}", "/a/{x}/{y...}", moreGeneral},

		// A non-final pattern segment can have one of two values: literal or
		// single wildcard. A final pattern segment can have one of 5: empty
		// (trailing slash), literal, dollar, single wildcard, or multi
		// wildcard. Trailing slash and multi wildcard are the same.

		// A literal should be more specific than anything it overlaps, except itself.
		{"/a", "/a", equivalent},
		{"/a", "/b", disjoint},
		{"/a", "/", moreSpecific},
		{"/a", "/{$}", disjoint},
		{"/a", "/{x}", moreSpecific},
		{"/a", "/{x...}", moreSpecific},

		// Adding a segment doesn't change that.
		{"/b/a", "/b/a", equivalent},
		{"/b/a", "/b/b", disjoint},
		{"/b/a", "/b/", moreSpecific},
		{"/b/a", "/b/{$}", disjoint},
		{"/b/a", "/b/{x}", moreSpecific},
		{"/b/a", "/b/{x...}", moreSpecific},
		{"/{z}/a", "/{z}/a", equivalent},
		{"/{z}/a", "/{z}/b", disjoint},
		{"/{z}/a", "/{z}/", moreSpecific},
		{"/{z}/a", "/{z}/{$}", disjoint},
		{"/{z}/a", "/{z}/{x}", moreSpecific},
		{"/{z}/a", "/{z}/{x...}", moreSpecific},

		// Single wildcard on left.
		{"/{z}", "/a", moreGeneral},
		{"/{z}", "/a/b", disjoint},
		{"/{z}", "/{$}", disjoint},
		{"/{z}", "/{x}", equivalent},
		{"/{z}", "/", moreSpecific},
		{"/{z}", "/{x...}", moreSpecific},
		{"/b/{z}", "/b/a", moreGeneral},
		{"/b/{z}", "/b/a/b", disjoint},
		{"/b/{z}", "/b/{$}", disjoint},
		{"/b/{z}", "/b/{x}", equivalent},
		{"/b/{z}", "/b/", moreSpecific},
		{"/b/{z}", "/b/{x...}", moreSpecific},

		// Trailing slash on left.
		{"/", "/a", moreGeneral},
		{"/", "/a/b", moreGeneral},
		{"/", "/{$}", moreGeneral},
		{"/", "/{x}", moreGeneral},
		{"/", "/", equivalent},
		{"/", "/{x...}", equivalent},

		{"/b/", "/b/a", moreGeneral},
		{"/b/", "/b/a/b", moreGeneral},
		{"/b/", "/b/{$}", moreGeneral},
		{"/b/", "/b/{x}", moreGeneral},
		{"/b/", "/b/", equivalent},
		{"/b/", "/b/{x...}", equivalent},

		{"/{z}/", "/{z}/a", moreGeneral},
		{"/{z}/", "/{z}/a/b", moreGeneral},
		{"/{z}/", "/{z}/{$}", moreGeneral},
		{"/{z}/", "/{z}/{x}", moreGeneral},
		{"/{z}/", "/{z}/", equivalent},
		{"/{z}/", "/a/", moreGeneral},
		{"/{z}/", "/{z}/{x...}", equivalent},
		{"/{z}/", "/a/{x...}", moreGeneral},
		{"/a/{z}/", "/{z}/a/", overlaps},

		// Multi wildcard on left.
		{"/{m...}", "/a", moreGeneral},
		{"/{m...}", "/a/b", moreGeneral},
		{"/{m...}", "/{$}", moreGeneral},
		{"/{m...}", "/{x}", moreGeneral},
		{"/{m...}", "/", equivalent},
		{"/{m...}", "/{x...}", equivalent},

		{"/b/{m...}", "/b/a", moreGeneral},
		{"/b/{m...}", "/b/a/b", moreGeneral},
		{"/b/{m...}", "/b/{$}", moreGeneral},
		{"/b/{m...}", "/b/{x}", moreGeneral},
		{"/b/{m...}", "/b/", equivalent},
		{"/b/{m...}", "/b/{x...}", equivalent},

		{"/{z}/{m...}", "/{z}/a", moreGeneral},
		{"/{z}/{m...}", "/{z}/a/b", moreGeneral},
		{"/{z}/{m...}", "/{z}/{$}", moreGeneral},
		{"/{z}/{m...}", "/{z}/{x}", moreGeneral},
		{"/{z}/{m...}", "/{w}/", equivalent},
		{"/{z}/{m...}", "/a/", moreGeneral},
		{"/{z}/{m...}", "/{z}/{x...}", equivalent},
		{"/{z}/{m...}", "/a/{x...}", moreGeneral},
		{"/a/{z}/{m...}", "/{z}/a/", overlaps},

		// Dollar on left.
		{"/{$}", "/a", disjoint},
		{"/{$}", "/a/b", disjoint},
		{"/{$}", "/{$}", equivalent},
		{"/{$}", "/{x}", disjoint},
		{"/{$}", "/", moreSpecific},
		{"/{$}", "/{x...}", moreSpecific},

		{"/b/{$}", "/b/a", disjoint},
		{"/b/{$}", "/b/a/b", disjoint},
		{"/b/{$}", "/b/{$}", equivalent},
		{"/b/{$}", "/b/{x}", disjoint},
		{"/b/{$}", "/b/", moreSpecific},
		{"/b/{$}", "/b/{x...}", moreSpecific},

		{"/{z}/{$}", "/{z}/a", disjoint},
		{"/{z}/{$}", "/{z}/a/b", disjoint},
		{"/{z}/{$}", "/{z}/{$}", equivalent},
		{"/{z}/{$}", "/{z}/{x}", disjoint},
		{"/{z}/{$}", "/{z}/", moreSpecific},
		{"/{z}/{$}", "/a/", overlaps},
		{"/{z}/{$}", "/a/{x...}", overlaps},
		{"/{z}/{$}", "/{z}/{x...}", moreSpecific},
		{"/a/{z}/{$}", "/{z}/a/", overlaps},
	} {
		pat1 := mustParse(t, test.p1)
		pat2 := mustParse(t, test.p2)
		if g := pat1.comparePaths(pat1); g != equivalent {
			t.Errorf("%s does not match itself; got %s", pat1, g)
		}
		if g := pat2.comparePaths(pat2); g != equivalent {
			t.Errorf("%s does not match itself; got %s", pat2, g)
		}
		got := pat1.comparePaths(pat2)
		if got != test.want {
			t.Errorf("%s vs %s: got %s, want %s", test.p1, test.p2, got, test.want)
		}
		var want2 relationship
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

func TestOverlapPath(t *testing.T) {
	for _, test := range []struct {
		p1, p2 string
		want   string
	}{
		{"/a/{x}", "/{x}/a", "/a/a"},
		{"/a/{z}/", "/{z}/a/", "/a/a/"},
		{"/a/{z}/{m...}", "/{z}/a/", "/a/a/"},
		{"/{z}/{$}", "/a/", "/a/"},
		{"/{z}/{$}", "/a/{x...}", "/a/"},
		{"/a/{z}/{$}", "/{z}/a/", "/a/a/"},
		{"/a/{x}/b/{y...}", "/{x}/c/{y...}", "/a/c/b/"},
		{"/a/{x}/b/", "/{x}/c/{y...}", "/a/c/b/"},
		{"/a/{x}/b/{$}", "/{x}/c/{y...}", "/a/c/b/"},
		{"/a/{z}/{x...}", "/{z}/b/{y...}", "/a/b/"},
	} {
		pat1 := mustParse(t, test.p1)
		pat2 := mustParse(t, test.p2)
		if pat1.comparePaths(pat2) != overlaps {
			t.Fatalf("%s does not overlap %s", test.p1, test.p2)
		}
		got := commonPath(pat1, pat2)
		if got != test.want {
			t.Errorf("%s vs. %s: got %q, want %q", test.p1, test.p2, got, test.want)
		}
	}
}

func TestDifferencePath(t *testing.T) {
	for _, test := range []struct {
		p1, p2 string
		want   string
	}{
		{"/a/{x}", "/{x}/a", "/a/x"},
		{"/{x}/a", "/a/{x}", "/x/a"},
		{"/a/{z}/", "/{z}/a/", "/a/z/"},
		{"/{z}/a/", "/a/{z}/", "/z/a/"},
		{"/{a}/a/", "/a/{z}/", "/ax/a/"},
		{"/a/{z}/{x...}", "/{z}/b/{y...}", "/a/z/"},
		{"/{z}/b/{y...}", "/a/{z}/{x...}", "/z/b/"},
		{"/a/b/", "/a/b/c", "/a/b/"},
		{"/a/b/{x...}", "/a/b/c", "/a/b/"},
		{"/a/b/{x...}", "/a/b/c/d", "/a/b/"},
		{"/a/b/{x...}", "/a/b/c/d/", "/a/b/"},
		{"/a/{z}/{m...}", "/{z}/a/", "/a/z/"},
		{"/{z}/a/", "/a/{z}/{m...}", "/z/a/"},
		{"/{z}/{$}", "/a/", "/z/"},
		{"/a/", "/{z}/{$}", "/a/x"},
		{"/{z}/{$}", "/a/{x...}", "/z/"},
		{"/a/{foo...}", "/{z}/{$}", "/a/foo"},
		{"/a/{z}/{$}", "/{z}/a/", "/a/z/"},
		{"/{z}/a/", "/a/{z}/{$}", "/z/a/x"},
		{"/a/{x}/b/{y...}", "/{x}/c/{y...}", "/a/x/b/"},
		{"/{x}/c/{y...}", "/a/{x}/b/{y...}", "/x/c/"},
		{"/a/{c}/b/", "/{x}/c/{y...}", "/a/cx/b/"},
		{"/{x}/c/{y...}", "/a/{c}/b/", "/x/c/"},
		{"/a/{x}/b/{$}", "/{x}/c/{y...}", "/a/x/b/"},
		{"/{x}/c/{y...}", "/a/{x}/b/{$}", "/x/c/"},
	} {
		pat1 := mustParse(t, test.p1)
		pat2 := mustParse(t, test.p2)
		rel := pat1.comparePaths(pat2)
		if rel != overlaps && rel != moreGeneral {
			t.Fatalf("%s vs. %s are %s, need overlaps or moreGeneral", pat1, pat2, rel)
		}
		got := differencePath(pat1, pat2)
		if got != test.want {
			t.Errorf("%s vs. %s: got %q, want %q", test.p1, test.p2, got, test.want)
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
		{"GET /", "/foo", false},
		{"/foo", "GET /", false},

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
		pat1 := mustParse(t, test.p1)
		pat2 := mustParse(t, test.p2)
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
		{"/", "GET /", false},
		{"/", "GET /foo", false},
		{"GET /", "GET /foo", false},
		{"GET /", "/foo", true},
	} {
		pat1 := mustParse(t, test.p1)
		pat2 := mustParse(t, test.p2)
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

func TestRegisterConflict(t *testing.T) {
	mux := NewServeMux()
	pat1 := "/a/{x}/"
	if err := mux.register(pat1, http.NotFoundHandler()); err != nil {
		t.Fatal(err)
	}
	pat2 := "/a/{y}/{z...}"
	err := mux.register(pat2, http.NotFoundHandler())
	var got string
	if err == nil {
		got = "<nil>"
	} else {
		got = err.Error()
	}
	q1 := regexp.QuoteMeta(pat1)
	q2 := regexp.QuoteMeta(pat2)
	wantre := `pattern "` + q2 +
		`" \(registered at .*/pattern_test.go:\d+\) conflicts with pattern "` +
		q1 +
		`" \(registered at .*/pattern_test.go:\d+\):
` + q2 + ` matches the same requests as ` + q1
	m, err := regexp.MatchString(wantre, got)
	if err != nil {
		t.Fatal(err)
	}
	if !m {
		t.Errorf("got\n%s\nwant\n%s", got, wantre)
	}
}

func TestDescribeRelationship(t *testing.T) {
	for _, test := range []struct {
		p1, p2 string
		want   string
	}{
		{"/a/{x}", "/a/{y}", "matches the same"},
		{"/a/{x}", "/{y}/b", "neither is more specific"},
		{"GET /", "/", "is more specific than"},
		{"/foo", "/", "is more specific than"},
		{"/", "GET /", "is more specific than"},
		{"/", "/foo", "is more specific than"},
		{"a.com/b", "/b", "does not have a host"},
		{"a.com/b", "b.com/b", "different hosts"},
	} {
		got := DescribeRelationship(test.p1, test.p2)
		if !strings.Contains(got, test.want) {
			t.Errorf("%s vs. %s:\ngot:\n%s\nwhich does not contain %q",
				test.p1, test.p2, got, test.want)
		}
	}
}

func mustParse(t *testing.T, s string) *Pattern {
	t.Helper()
	p, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
