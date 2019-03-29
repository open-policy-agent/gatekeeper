package target

test_undefined_kind_selector_any_group_kind {
	any_kind_selector_matches({}) with input as pod_review
}

test_wildcard_kind_selector_empty_group {
	any_kind_selector_matches({
    "kinds": [
      {"apiGroups": ["*"], "kinds": ["*"]},
    ]
	}) with input as pod_review
}

test_wildcard_kind_selector_nonempty_group {
	any_kind_selector_matches({
    "kinds": [
      {"apiGroups": ["*"], "kinds": ["*"]},
    ]
	}) with input as foo_review
}

test_empty_group_kind_selector {
	any_kind_selector_matches({
    "kinds": [
      {"apiGroups": [""], "kinds": ["*"]},
    ]
	}) with input as pod_review
}

test_empty_group_kind_selector_negative {
	not any_kind_selector_matches({
    "kinds": [
      {"apiGroups": [""], "kinds": ["*"]},
    ]
	}) with input as foo_review
}

test_empty_group_constant_kind_kind_selector {
	any_kind_selector_matches({
    "kinds": [
      {"apiGroups": [""], "kinds": ["Pod"]},
    ]
	}) with input as pod_review
}

test_nonempty_group_kind_selector {
	any_kind_selector_matches({
    "kinds": [
      {"apiGroups": ["example.com"], "kinds": ["*"]},
    ]
	}) with input as foo_review
}

test_nonempty_group_kind_selector_negative {
	not any_kind_selector_matches({
    "kinds": [
      {"apiGroups": ["example.com"], "kinds": ["*"]},
    ]
	}) with input as pod_review
}


test_nonempty_group_constant_kind_kind_selector {
	any_kind_selector_matches({
    "kinds": [
      {"apiGroups": ["example.com"], "kinds": ["Foo"]},
    ]
	}) with input as foo_review
}

test_nonempty_group_constant_kind_kind_selector_negative {
	not any_kind_selector_matches({
    "kinds": [
      {"apiGroups": ["example.com"], "kinds": ["Foo"]},
    ]
	}) with input as bar_review
}

pod_review = {
  "review": {
    "kind": {
      "group": "",
      "kind": "Pod"
    }
  }
}

foo_review = {
  "review": {
    "kind": {
      "group": "example.com",
      "kind": "Foo"
    }
  }
}

bar_review = {
  "review": {
    "kind": {
      "group": "example.com",
      "kind": "Bar"
    }
  }
}