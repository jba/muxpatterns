# muxpatterns

This repo holds a sample implementation of the enhanced http.ServeMux routing
patterns that are being discussed at https://github.com/golang/go/discussions/60227.

The `ServeMux` type should behave like `http.ServeMux` with the additional features
described in the discussion top post.

The `Pattern` type is used internally. It is exported here for experimentation.

The `DescribeRelationship` function will not be part of the proposed API, but it
can help in understanding how patterns are related.
