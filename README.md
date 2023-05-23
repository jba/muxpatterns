# muxpatterns

This repo holds a sample implementation of the enhanced http.ServeMux routing
patterns that are being discussed at https://github.com/golang/go/discussions/60227.

## Justfication for precedence rule 4.

Patterns ending in '/' behave as if they ended in an anonymous {...} wildcard,
so we'll treat them as such below.

In the absence of {...} wildcards, two patterns can only conflict if they have
the same number of segments, and either both end in {$} or neither do.

If neither pattern ends in {$} or if both do then they can only match paths with
the same number of segments. So if the patterns' number of segments differ, so
must those of any matching paths.

If only one ends in {$}, then it can only match paths that end in '/', and the
other pattern can only match paths that don't end in '/', so they cannot
conflict.

So in the absence of {...} wildcards, the longest-literal-prefix rule (rule 3)
suffices to order conflicting paths.

So now let's think  about patterns ending in {...}.

Consider the pattern "/" (which again we can think of as ending in {...}). In
the current world of literal patterns, every other pattern beats it. We want to
preserve that property: "/" is a catch-all for any path not otherwise matched. A
pattern that ends in {...} should not conflict with any other pattern with the
same prefix (the part before for the {...} wildcard) and should lose to all of
them. The only exceptions are patterns that are effectively identical because
they differ only in the names of variables. For example "/a/{x}/{y...}"
conflicts with "/a/{z}/" because they are essentially two names for the same
pattern.

Here are some examples of patterns that should win over "/":

    1. /{$}
    2. /a
    3. /{x}
    4. /a/{x}
    5. /a/{x...}
    6. /{x}/{y...}

Examples 2, 4 and 5 win by rule 3, but the others require another rule.

I considered the rule "patterns that don't have a {...} wildcard win over those
that do," but that fails for examples 5 and 6. It would also say that "/{x}/a/b"
wins over "/{x}/{y}/{z...}", which isn't totally unreasonable but also isn't
what we had in mind: it would be odd if that were true but "{x}/a/b" did not win
over "{x}/{y}/b".


## Specificity

/a/

/a/b
