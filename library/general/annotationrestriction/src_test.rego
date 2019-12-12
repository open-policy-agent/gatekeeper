package requiredannotations

test_input_correct_annotations {
    input := { "review": review_ingress({"anno1": "annoval1"}), "parameters": {"annotations": [annotation("anno1", "annoval1")]}}
    results := violation with input as input
    count(results) == 0
}

test_no_required_annotations {
    input := { "review": review_ingress({"anno": "annoval"}), "parameters": {}}
    results := violation with input as input
    count(results) == 0
}

test_no_required_annotations {
    input := { "review": review_ingress({}), "parameters": {}}
    results := violation with input as input
    count(results) == 0
}

test_input_has_extra_annotation {
    input := { "review": review_ingress({"anno1": "annoval1", "anno2": "annoval2"}), "parameters": {"annotations": [annotation("anno1", "annoval1")]}}
    results := violation with input as input
    count(results) == 0
}

test_2_required_annotations {
    input := { "review": review_ingress({"anno1": "annoval1", "anno2": "annoval2"}), "parameters": {"annotations": [annotation("anno1", "annoval1"), annotation("anno2", "annoval2")]}}
    results := violation with input as input
    count(results) == 0
}

test_input_missing_annotation {
    input := { "review": review_ingress({"anno": "annoval"}), "parameters": {"annotations": [annotation("anno1", "annoval1")]}}
    results := violation with input as input
    count(results) == 1
}

test_input_wrong_value {
    input := { "review": review_ingress({"anno1": "annoval1"}), "parameters": {"annotations": [annotation("anno1", "other_val")]}}
    results := violation with input as input
    count(results) == 1
}

test_input_missing_annotation_req2 {
    input := { "review": review_ingress({"anno1": "annoval1"}), "parameters": {"annotations": [annotation("anno1", "annoval1"), annotation("anno2", "annoval2")]}}
    results := violation with input as input
    count(results) == 1
}

test_input_no_annotations {
    input := { "review": empty_ingress, "parameters": {"annotations": [annotation("anno1", "annoval1")]}}
    results := violation with input as input
    count(results) == 1
}

test_input_no_annotations_req2 {
    input := { "review": empty_ingress, "parameters": {"annotations": [annotation("anno1", "annoval1"), annotation("anno2", "annoval2")]}}
    results := violation with input as input
    count(results) == 1
}

test_input_two_wrong {
    input := { "review": review_ingress({"anno1": "not", "anno2": "correct"}), "parameters": {"annotations": [annotation("anno1", "annoval1"), annotation("anno2", "annoval2")]}}
    results := violation with input as input
    count(results) == 2
}

empty_ingress = out {
  out = {
    "object": {
      "kind": "Ingress",
      "apiVersion": "extensions/v1beta1",
      "metadata": {
        "name": "my-ingress"
      }
    }
  }
}

review_ingress(annotations) = out {
  out = {
    "object": {
      "kind": "Ingress",
      "apiVersion": "extensions/v1beta1",
      "metadata": {
        "name": "my-ingress",
        "annotations": annotations
      }
    }
  }
}

annotation(key, dval) = out {
  out = {"key": key, "desiredValue": dval}
}
