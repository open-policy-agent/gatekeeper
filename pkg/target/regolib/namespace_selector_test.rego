package target

test_name_match {
  matches_namespaces({"namespaces": ["match"]})
    with input.review.kind as pod_kind
    with input.review.namespace as "match"
}

test_name_no_match {
  not matches_namespaces({"namespaces": ["match"]})
    with input.review.kind as pod_kind
    with input.review.namespace as "no-match"
}

test_name_match_is_ns {
  matches_namespaces({"namespaces": ["match"]})
    with input.review.kind as ns_kind
    with input.review.object.metadata.name as "match"
}

test_name_no_match_is_ns {
  not matches_namespaces({"namespaces": ["match"]})
    with input.review.kind as ns_kind
    with input.review.object.metadata.name as "no-match"
}

test_sideload_match {
  matches_nsselector({"namespaceSelector": {"matchLabels": {"hi": "there"}}})
    with input.review.kind as pod_kind
    with input.review._unstable.namespace as {"metadata": {"labels": {"hi": "there"}}}
}

test_sideload_no_match {
  not matches_nsselector({"namespaceSelector": {"matchLabels": {"hi": "there"}}})
    with input.review.kind as pod_kind
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
