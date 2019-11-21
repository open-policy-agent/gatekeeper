package requiredannotations

test_input_has_correct_value {
    input := {"review": review_ingress(annotation("ELBSecurityPolicy-TLS-1-2-2017-01"))}
    results := violation with input as input
    count(results) == 0
}

test_input_has_incorrect_value {
    input := {"review": review_ingress(annotation("policy"))}
    results := violation with input as input
    count(results) == 1
}

test_missing_annotations {
    input := {"review": review_ingress({})}
    results := violation with input as input
    count(results) == 1
}


review_ingress(annotationVal) = out {
  out = {
    "object": {
      "kind": "Ingress",
      "apiVersion": "extensions/v1beta1",
      "metadata": {
        "name": "my-ingress",
        "annotations": annotationVal
      }
    }
  }
}



annotation(val) = out {
  out = {
    "alb.ingress.kubernetes.io/ssl-policy": val
  }
}