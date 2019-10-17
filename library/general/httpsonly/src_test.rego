package k8shttpsonly

test_http_disallowed {
    input := {"review": review_ingress(annotation("false"), tls)}
    results := violation with input as input
    count(results) == 0
}
test_boolean_annotation {
    input := {"review": review_ingress(annotation(false), tls)}
    results := violation with input as input
    count(results) == 1
}
test_true_annotation {
    input := {"review": review_ingress(annotation("true"), tls)}
    results := violation with input as input
    count(results) == 1
}
test_missing_annotation {
    input := {"review": review_ingress({}, tls)}
    results := violation with input as input
    count(results) == 1
}
test_empty_tls {
    input := {"review": review_ingress({}, empty_tls)}
    results := violation with input as input
    count(results) == 1
}
test_missing_tls {
    input := {"review": review_ingress(annotation("false"), {})}
    results := violation with input as input
    count(results) == 1
}
test_missing_all {
    input := {"review": review_ingress({}, {})}
    results := violation with input as input
    count(results) == 1
}

review_ingress(annotationVal, tlsVal) = out {
  out = {
    "object": {
      "kind": "Ingress",
      "apiVersion": "extensions/v1beta1",
      "metadata": {
        "name": "my-ingress",
        "annotations": annotationVal
      },
      "spec": tlsVal
    }
  }
}

annotation(val) = out {
  out = {
    "kubernetes.io/ingress.allow-http": val
  }
}

empty_tls = {
  "tls": []
}

tls = {
  "tls": [{"secretName": "secret-cert"}]
}
