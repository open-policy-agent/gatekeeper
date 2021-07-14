package target

test_name_match {
  matches_namespaces({"namespaces": ["kube-system", "gatekeeper-system"]})
    with input.review.kind as pod_kind
    with input.review.namespace as "gatekeeper-system"
}

test_name_no_match {
  not matches_namespaces({"namespaces": ["kube-system", "gatekeeper-system"]})
    with input.review.kind as pod_kind
    with input.review.namespace as "burrito"
}

test_name_match_is_ns {
  matches_namespaces({"namespaces": ["kube-system", "gatekeeper-system"]})
    with input.review.kind as ns_kind
    with input.review.object.metadata.name as "gatekeeper-system"
}

test_name_no_match_is_ns {
  not matches_namespaces({"namespaces": ["kube-system", "gatekeeper-system"]})
    with input.review.kind as ns_kind
    with input.review.object.metadata.name as "front-end"
}

test_prefix_match {
  matches_namespaces({"namespaces": ["taco", "burrito", "kube-*", "gatekeeper-*"]})
    with input.review.kind as pod_kind
    with input.review.namespace as "kube-system"
}

test_prefix_no_match {
  not matches_namespaces({"namespaces": ["front-end", "kube-*", "gatekeeper-*"]})
    with input.review.kind as pod_kind
    with input.review.namespace as "back-end"
}

test_sideload_match {
  matches_nsselector({"namespaceSelector": {"matchLabels": {"hi": "there"}}})
    with input.review.kind as pod_kind
    with input.review.namespace as "my_namespace"
    with input.review._unstable.namespace as {"metadata": {"labels": {"hi": "there"}}}
}

test_sideload_no_match {
  not matches_nsselector({"namespaceSelector": {"matchLabels": {"hi": "there"}}})
    with input.review.kind as pod_kind
    with input.review.namespace as "my_namespace"
    with input.review._unstable.namespace as {"metadata": {"labels": {"bye": "there"}}}
}


test_cache_match {
  matches_nsselector({"namespaceSelector": {"matchLabels": {"hi": "there"}}})
    with data["{{.DataRoot}}"].cluster["v1"]["Namespace"]["my_namespace"] as {"metadata": {"labels": {"hi": "there"}}}
    with input.review.kind as pod_kind
    with input.review.namespace as "my_namespace"
}

test_cache_no_match {
  not matches_nsselector({"namespaceSelector": {"matchLabels": {"bye": "there"}}})
    with data["{{.DataRoot}}"].cluster["v1"]["Namespace"]["my_namespace"] as {"metadata": {"labels": {"hi": "there"}}}
    with input.review.kind as pod_kind
    with input.review.namespace as "my_namespace"
}

pod_kind = {
  "group": "",
  "kind": "Pod"
}


ns_kind = {
  "group": "",
  "kind": "Namespace"
}


ns_match_obj = {
  "metadata": {
    "labels": {
      "match": "yes"
    }
  }
}

ns_no_match_obj = {
  "metadata": {
    "labels": {
      "match": "no"
    }
  }
}

test_direct_negative_match {
  matches_nsselector({"namespaceSelector": {"matchExpressions": [{"key": "match", "operator": "NotIn", "values": ["no"]}]}})
    with input.review.kind as ns_kind
    with input.review.object as ns_match_obj
}

test_direct_no_negative_match {
  not matches_nsselector({"namespaceSelector": {"matchExpressions": [{"key": "match", "operator": "NotIn", "values": ["no"]}]}})
    with input.review.kind as ns_kind
    with input.review.object as ns_no_match_obj
}

test_direct_negative_match_oldobject {
  matches_nsselector({"namespaceSelector": {"matchExpressions": [{"key": "match", "operator": "NotIn", "values": ["no"]}]}})
    with input.review.kind as ns_kind
    with input.review.oldObject as ns_match_obj
}

test_direct_no_negative_match_oldobject {
  not matches_nsselector({"namespaceSelector": {"matchExpressions": [{"key": "match", "operator": "NotIn", "values": ["no"]}]}})
    with input.review.kind as ns_kind
    with input.review.oldObject as ns_no_match_obj
}

test_exclude_not_provided {
  does_not_match_excludednamespaces({})
    with input.review.kind as pod_kind
    with input.review.namespace as "baz"
}

test_exclude_cluster_scoped {
  does_not_match_excludednamespaces({"excludedNamespaces": ["foo", "bar"]})
    with input.review.kind as pod_kind
}

test_exclude_namespaced_no_match {
  does_not_match_excludednamespaces({"excludedNamespaces": ["foo", "bar", "kube-*"]})
    with input.review.kind as pod_kind
    with input.review.namespace as "baz"
}

test_exclude_namespaced_match {
  not does_not_match_excludednamespaces({"excludedNamespaces": ["foo", "bar"]})
    with input.review.kind as pod_kind
    with input.review.namespace as "bar"
}

test_exclude_namespaced_wildcard_match {
  not does_not_match_excludednamespaces({"excludedNamespaces": ["kube-*", "bar"]})
    with input.review.kind as pod_kind
    with input.review.namespace as "kube-system"
}

test_exclude_not_provided_ns {
  does_not_match_excludednamespaces({})
    with input.review.kind as ns_kind
    with input.review.object.metadata.name as "match"
}

test_exclude_namespaced_match_ns {
  not does_not_match_excludednamespaces({"excludedNamespaces": ["foo", "bar"]})
    with input.review.kind as ns_kind
    with input.review.object.metadata.name as "foo"
}

test_exclude_namespaced_no_match_ns {
  does_not_match_excludednamespaces({"excludedNamespaces": ["foo", "bar"]})
    with input.review.kind as ns_kind
    with input.review.object.metadata.name as "no-match"
}
