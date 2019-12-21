package target

# Test object/oldobject

matching_object = {
  "metadata": {"labels": {"match": "yes"}}
}

non_matching_object = {
  "metadata": {"labels": {"match": "no"}}
}

test_object_only_match {
  any_labelselector_match({"matchLabels": {"match": "yes"}}) with input.review.object as matching_object
}

test_object_only_non_match {
  not any_labelselector_match({"matchLabels": {"match": "yes"}}) with input.review.object as non_matching_object
}

test_old_object_only_match {
  any_labelselector_match({"matchLabels": {"match": "yes"}}) with input.review.oldObject as matching_object
}

test_old_object_only_non_match {
  not any_labelselector_match({"matchLabels": {"match": "yes"}}) with input.review.oldObject as non_matching_object
}

test_obj_mix_both_match {
  any_labelselector_match({"matchLabels": {"match": "yes"}}) with input.review as {"object": matching_object, "oldObject": matching_object}
}

test_obj_mix_old_match {
  any_labelselector_match({"matchLabels": {"match": "yes"}}) with input.review as {"object": non_matching_object, "oldObject": matching_object}
}

test_obj_mix_new_match {
  any_labelselector_match({"matchLabels": {"match": "yes"}}) with input.review as {"object": matching_object, "oldObject": non_matching_object}
}

test_obj_mix_no_match {
  not any_labelselector_match({"matchLabels": {"match": "yes"}}) with input.review as {"object": non_matching_object, "oldObject": non_matching_object}
}

# Test empty cases

test_empty_selector_matches_empty_labelset {
  matches_label_selector({}, {})
}

test_empty_selector_matches_labelset {
  matches_label_selector({}, {"a": "b"})
}


# Test matchLabels

test_selector_matches_labelset_size_1 {
  matches_label_selector({"matchLabels": {"a": "b"}}, {"a": "b"})
}

test_selector_matches_labelset_size_3 {
  matches_label_selector({"matchLabels": {"a": "b", "c": "d", "e": "f"}}, {"a": "b", "c": "d", "e": "f"})
}

test_selector_matches_labelset_extra_labels {
  matches_label_selector({"matchLabels": {"a": "b"}}, {"a": "b", "c": "d", "e": "f"})
}

test_selector_misses_empty_labelset {
  not matches_label_selector({"matchLabels": {"a": "b"}}, {})
}

test_selector_misses_off_by_1 {
  not matches_label_selector({"matchLabels": {"a": "b", "c": "d", "e": "f"}}, {"a": "b", "c": "d"})
}


# Test expression operator In

test_expression_in_1_value {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "In",
    "values": ["b"]
   }]}
  matches_label_selector(expr, {"a": "b"})
}

test_expression_in_3_values {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "In",
    "values": ["a", "b", "c"]
   }]}
  matches_label_selector(expr, {"a": "a"})
  matches_label_selector(expr, {"a": "b"})
  matches_label_selector(expr, {"a": "c"})
}

test_expression_in_3_values_extra_value {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "In",
    "values": ["a", "b", "c"]
   }]}
  matches_label_selector(expr, {"a": "a", "b": "b"})
  matches_label_selector(expr, {"a": "b", "b": "b"})
  matches_label_selector(expr, {"a": "c", "b": "b"})
}

test_expression_in_1_values_violation_no_labels {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "In",
    "values": ["b"]
   }]}
  not matches_label_selector(expr, {})
}

test_expression_in_3_values_violation_no_labels {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "In",
    "values": ["a", "b", "c"]
   }]}
  not matches_label_selector(expr, {})
}

test_expression_in_1_values_violation_wrong_value {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "In",
    "values": ["a"]
   }]}
  not matches_label_selector(expr, {"a": "r"})
}

test_expression_in_1_values_violation_wrong_label {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "In",
    "values": ["a"]
   }]}
  not matches_label_selector(expr, {"r": "a"})
}

test_expression_in_3_values_violation_wrong_value {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "In",
    "values": ["a", "b", "c"]
   }]}
  not matches_label_selector(expr, {"a": "r"})
}

test_expression_in_3_values_violation_wrong_label {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "In",
    "values": ["a", "b", "c"]
   }]}
  not matches_label_selector(expr, {"r": "a"})
}


# Test expression operator NotIn

test_expression_notin_1_value {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "NotIn",
    "values": ["b"]
   }]}
  matches_label_selector(expr, {"a": "a"})
}

test_expression_notin_3_values {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "NotIn",
    "values": ["a", "b", "c"]
   }]}
  matches_label_selector(expr, {"a": "r"})
  matches_label_selector(expr, {"a": "f"})
  matches_label_selector(expr, {"a": "q"})
}

test_expression_notin_3_values_extra_value {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "NotIn",
    "values": ["a", "b", "c"]
   }]}
  matches_label_selector(expr, {"a": "r", "b": "b"})
  matches_label_selector(expr, {"a": "f", "b": "b"})
  matches_label_selector(expr, {"a": "q", "b": "b"})
}

test_expression_notin_1_values_no_labels {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "NotIn",
    "values": ["b"]
   }]}
  matches_label_selector(expr, {})
}

test_expression_notin_3_values_no_labels {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "NotIn",
    "values": ["a", "b", "c"]
   }]}
  matches_label_selector(expr, {})
}

test_expression_notin_1_values_wrong_label {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "NotIn",
    "values": ["a"]
   }]}
  matches_label_selector(expr, {"r": "a"})
}

test_expression_in_3_values_wrong_label {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "NotIn",
    "values": ["a", "b", "c"]
   }]}
  matches_label_selector(expr, {"r": "a"})
}

test_expression_notin_1_values_violation_wrong_value {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "NotIn",
    "values": ["a"]
   }]}
  not matches_label_selector(expr, {"a": "a"})
}


test_expression_notin_3_values_violation_wrong_value {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "NotIn",
    "values": ["a", "b", "c"]
   }]}
  not matches_label_selector(expr, {"a": "a"})
  not matches_label_selector(expr, {"a": "b"})
  not matches_label_selector(expr, {"a": "c"})
}


# Test expression Exists
test_expression_exists_1_key {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "Exists",
   }]}
   matches_label_selector(expr, {"a": "a"})
}

test_expression_exists_3_keys {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "Exists",
   }]}
   matches_label_selector(expr, {"a": "a", "b": "b", "c": "c"})
}

test_expression_exists_violation_3_keys {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "Exists",
   }]}
   not matches_label_selector(expr, {"r": "a", "b": "b", "c": "c"})
}

test_expression_exists_violation_1_key {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "Exists",
   }]}
   not matches_label_selector(expr, {"r": "a"})
}

test_expression_exists_violation_no_keys {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "Exists",
   }]}
   not matches_label_selector(expr, {})
}

test_expression_exists_values_ignored {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "Exists",
    "values": "a"
   }]}
   matches_label_selector(expr, {"a": "b"})
}

test_expression_exists_violation_values_ignored {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "Exists",
    "values": "a"
   }]}
   not matches_label_selector(expr, {"b": "a"})
}


# Test expression DoesNotExist
test_expression_doesnotexist_no_keys {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "DoesNotExist",
   }]}
   matches_label_selector(expr, {})
}

test_expression_doesnotexist_1_key {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "DoesNotExist",
   }]}
   matches_label_selector(expr, {"b": "b"})
}

test_expression_doesnotexist_3_keys {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "DoesNotExist",
   }]}
   matches_label_selector(expr, {"b": "b", "c": "c", "d": "d"})
}

test_expression_doesnotexist_violation_1_key {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "DoesNotExist",
   }]}
   not matches_label_selector(expr, {"a": "b"})
}

test_expression_doesnotexist_violation_3_keys {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "DoesNotExist",
   }]}
   not matches_label_selector(expr, {"a": "b", "b": "b", "c": "c"})
}

test_expression_doesnotexist_values_ignored {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "DoesNotExist",
    "values": "a"
   }]}
   matches_label_selector(expr, {"b": "a"})
}

test_expression_doesnotexist_violation_values_ignored {
  expr := {"matchExpressions": [{
    "key": "a",
    "operator": "DoesNotExist",
    "values": "a"
   }]}
   not matches_label_selector(expr, {"a": "b"})
}


# Test compound uses
test_compound_selector_multi_success {
  expr := {"matchExpressions": [
    {
      "key": "a",
      "operator": "DoesNotExist",
    },
    {
      "key": "b",
      "operator": "Exists",
    },
    {
      "key": "c",
      "operator": "In",
      "values": ["z", "x", "y"]
    },
    {
      "key": "d",
      "operator": "NotIn",
      "values": ["z", "x", "y"]
    },
   ]}
   matches_label_selector(expr, {"b": "a", "c": "z", "d": "r"})
}

test_compound_selector_one_violation {
  expr := {"matchExpressions": [
    {
      "key": "a",
      "operator": "DoesNotExist",
    },
    {
      "key": "b",
      "operator": "Exists",
    },
    {
      "key": "c",
      "operator": "In",
      "values": ["z", "x", "y"]
    },
    {
      "key": "d",
      "operator": "NotIn",
      "values": ["z", "x", "y"]
    },
   ]}
   not matches_label_selector(expr, {"a": "z", "b": "a", "c": "z", "d": "r"})
   not matches_label_selector(expr, {"c": "z", "d": "r"})
   not matches_label_selector(expr, {"b": "a", "c": "r", "d": "r"})
   not matches_label_selector(expr, {"b": "a", "c": "z", "d": "x"})
}

test_compound_selector_many_violations {
  expr := {"matchExpressions": [
    {
      "key": "a",
      "operator": "DoesNotExist",
    },
    {
      "key": "b",
      "operator": "Exists",
    },
    {
      "key": "c",
      "operator": "In",
      "values": ["z", "x", "y"]
    },
    {
      "key": "d",
      "operator": "NotIn",
      "values": ["z", "x", "y"]
    },
   ]}
   not matches_label_selector(expr, {"a": "z"})
}

test_double_compound_selector_multi_success {
  expr := {
    "matchLabels": {
      "e": "f"
    },
    "matchExpressions": [
      {
        "key": "a",
        "operator": "DoesNotExist",
      },
      {
        "key": "b",
        "operator": "Exists",
      },
      {
        "key": "c",
        "operator": "In",
        "values": ["z", "x", "y"]
      },
      {
        "key": "d",
        "operator": "NotIn",
        "values": ["z", "x", "y"]
      },
   ]}
   matches_label_selector(expr, {"b": "a", "c": "z", "d": "r", "e": "f"})
}

test_double_compound_selector_label_failure {
  expr := {
    "matchLabels": {
      "e": "f"
    },
    "matchExpressions": [
      {
        "key": "a",
        "operator": "DoesNotExist",
      },
      {
        "key": "b",
        "operator": "Exists",
      },
      {
        "key": "c",
        "operator": "In",
        "values": ["z", "x", "y"]
      },
      {
        "key": "d",
        "operator": "NotIn",
        "values": ["z", "x", "y"]
      },
   ]}
   not matches_label_selector(expr, {"b": "a", "c": "z", "d": "r", "e": "r"})
   not matches_label_selector(expr, {"b": "a", "c": "z", "d": "r"})
}

test_double_compound_selector_expression_failure {
  expr := {
    "matchLabels": {
      "e": "f"
    },
    "matchExpressions": [
      {
        "key": "a",
        "operator": "DoesNotExist",
      },
      {
        "key": "b",
        "operator": "Exists",
      },
      {
        "key": "c",
        "operator": "In",
        "values": ["z", "x", "y"]
      },
      {
        "key": "d",
        "operator": "NotIn",
        "values": ["z", "x", "y"]
      },
   ]}
   not matches_label_selector(expr, {"a": "r", "b": "a", "c": "z", "d": "r", "e": "f"})
   not matches_label_selector(expr, {"c": "z", "d": "r", "e": "f"})
   not matches_label_selector(expr, {"b": "a", "c": "r", "d": "r", "e": "f"})
   not matches_label_selector(expr, {"b": "a", "c": "z", "d": "x", "e": "f"})
   not matches_label_selector(expr, {"a": "r", "d": "x", "e": "f"})
}

test_double_compound_selector_expression_all_failure {
  expr := {
    "matchLabels": {
      "e": "f"
    },
    "matchExpressions": [
      {
        "key": "a",
        "operator": "DoesNotExist",
      },
      {
        "key": "b",
        "operator": "Exists",
      },
      {
        "key": "c",
        "operator": "In",
        "values": ["z", "x", "y"]
      },
      {
        "key": "d",
        "operator": "NotIn",
        "values": ["z", "x", "y"]
      },
   ]}
   not matches_label_selector(expr, {"a": "r", "b": "a", "c": "z", "d": "r", "e": "x"})
   not matches_label_selector(expr, {"c": "z", "d": "r", "e": "x"})
   not matches_label_selector(expr, {"b": "a", "c": "r", "d": "r"})
   not matches_label_selector(expr, {"b": "a", "c": "z", "d": "x", "e": "x"})
   not matches_label_selector(expr, {"a": "r", "d": "x"})
}

test_double_compound_selector_expression_empty_failure {
  expr := {
    "matchLabels": {
      "e": "f"
    },
    "matchExpressions": [
      {
        "key": "a",
        "operator": "DoesNotExist",
      },
      {
        "key": "b",
        "operator": "Exists",
      },
      {
        "key": "c",
        "operator": "In",
        "values": ["z", "x", "y"]
      },
      {
        "key": "d",
        "operator": "NotIn",
        "values": ["z", "x", "y"]
      },
   ]}
   not matches_label_selector(expr, {})
}
