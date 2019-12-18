package target

test_no_nsselector {
  res := autoreject_review
    with data["{{.ConstraintsRoot}}"].a.b.spec.match as {}
    with input.review.namespace as "testns"

   count(res) == 0
}

test_with_nsselector {
  res := autoreject_review
    with data["{{.ConstraintsRoot}}"].a.b.spec.match.namespaceSelector as {}
    with input.review.namespace as "testns"

   count(res) == 1
}

test_with_empty_ns {
  res := autoreject_review
    with data["{{.ConstraintsRoot}}"].a.b.spec.match.namespaceSelector as {}
    with input.review.namespace as ""

   count(res) == 0
}

test_with_undefined_ns {
  res := autoreject_review
    with data["{{.ConstraintsRoot}}"].a.b.spec.match.namespaceSelector as {}
    with input.review as {}

   count(res) == 0
}

test_with_cached_ns {
 res := autoreject_review
   with data["{{.ConstraintsRoot}}"].a.b.spec.match.namespaceSelector as {}
   with input.review.namespace as "testns"
   with data["{{.DataRoot}}"].cluster["v1"]["Namespace"].testns as {}

  count(res) == 0
}

test_with_sideloaded_ns {
 res := autoreject_review
   with data["{{.ConstraintsRoot}}"].a.b.spec.match.namespaceSelector as {}
   with input.review.namespace as "testns"
   with input.review._unstable.namespace as {}

  count(res) == 0
}

