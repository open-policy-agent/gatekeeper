package k8srequiredlabels

test_input_no_required_labels {
    input := { "review": review({"some": "label"}), "parameters": {}}
    results := violation with input as input
    count(results) == 0
}
test_input_no_required_labels {
    input := { "review": empty, "parameters": {}}
    results := violation with input as input
    count(results) == 0
}
test_input_has_label {
    input := { "review": review({"some": "label"}), "parameters": {"labels": [lbl("some", "label")]}}
    results := violation with input as input
    count(results) == 0
}
test_input_has_extra_label {
    input := { "review": review({"some": "label", "new": "thing"}), "parameters": {"labels": [lbl("some", "label")]}}
    results := violation with input as input
    count(results) == 0
}
test_input_has_extra_label_req2 {
    input := { "review": review({"some": "label", "new": "thing"}), "parameters": {"labels": [lbl("some", "label"), lbl("new", "thing")]}}
    results := violation with input as input
    count(results) == 0
}
test_input_missing_label {
    input := { "review": review({"some_other": "label"}), "parameters": {"labels": [lbl("some", "label")]}}
    results := violation with input as input
    count(results) == 1
}
test_input_wrong_value {
    input := { "review": review({"some": "label2"}), "parameters": {"labels": [lbl("some", "label$")]}}
    results := violation with input as input
    count(results) == 1
}
test_input_one_missing {
    input := { "review": review({"some": "label"}), "parameters": {"labels": [lbl("some", "label"), lbl("other", "label")]}}
    results := violation with input as input
    count(results) == 1
}
test_input_wrong_empty {
    input := { "review": empty, "parameters": {"labels": [lbl("some", "label$")]}}
    results := violation with input as input
    count(results) == 1
}
test_input_two_missing {
    input := { "review": empty, "parameters": {"labels": [lbl("some", "label"), lbl("other", "label")]}}
    results := violation with input as input
    count(results) == 1
}
test_input_two_wrong {
    input := { "review": review({"some": "lbe", "other": "lbe"}), "parameters": {"labels": [lbl("some", "label"), lbl("other", "label")]}}
    results := violation with input as input
    count(results) == 2
}
test_input_two_allowed {
    input := { "review": review({"some": "gray", "other": "grey"}), "parameters": {"labels": [lbl("some", "gr[ae]y"), lbl("other", "gr[ae]y")]}}
    results := violation with input as input
    count(results) == 0
}
test_input_message {
    input := { "review": review({"some": "label2"}), "parameters": {"message": "WRONG_VALUE", "labels": [lbl("some", "label$")]}}
    results := violation with input as input
    results[_].msg == "WRONG_VALUE"
}

empty = {
  "object": {
    "metadata": {
      "name": "nginx"
    },
  }

}

review(labels) = output {
  output = {
    "object": {
      "metadata": {
        "name": "nginx",
        "labels": labels,
      },
    }
  }
}

lbl(k, v) = out {
  out = {"key": k, "allowedRegex": v}
}
