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

test_wildcard_name_match {
  matches_name({"name": "foo*"})
    with input.review.name as "foobar"
}

test_wildcard_no_asterisk_no_match {
  not matches_name({"name": "foo"})
    with input.review.name as "foobar"
}
