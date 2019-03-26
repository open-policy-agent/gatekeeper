package target

# has_field tests
test_has_field_exists {
  obj := {"a": "b"}
  true == has_field(obj, "a")
}

# False is a tricky special case, as false responses would create an undefined document unless
# they are explicitly tested for
test_has_field_false {
  obj := {"a": false}
  true == has_field(obj, "a")
}

test_has_field_no_field {
  obj := {}
  false == has_field(obj, "a")
}


# get_default_tests
test_get_default_exists {
  obj := {"a": "b"}
  "b" == get_default(obj, "a", "q")
}

test_get_default_not_exists {
  obj := {}
  "q" == get_default(obj, "a", "q")
}

test_get_default_has_false {
  obj := {"a": false}
  false == get_default(obj, "a", "b")
}