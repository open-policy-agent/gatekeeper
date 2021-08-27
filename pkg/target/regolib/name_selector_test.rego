package target

test_name_match {
  matches_name({"name": "foo"})
    with input.review.name as "foo"
}

test_name_no_match {
  not matches_name({"name": "bar"})
    with input.review.name as "foo"
}

test_no_name_is_match {
  matches_name({})
    with input.review.name as "foo"
}

# JULIAN - Will we ever situation where match.name is "" ?
