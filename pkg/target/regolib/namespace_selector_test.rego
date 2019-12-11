package target

test_sideload_match {
  matches_nsselector({"namespaceSelector": {"matchLabels": {"hi": "there"}}}) with input.review._unstable.namespace as {"metadata": {"labels": {"hi": "there"}}}
}

test_sideload_no_match {
  not matches_nsselector({"namespaceSelector": {"matchLabels": {"hi": "there"}}}) with input.review._unstable.namespace as {"metadata": {"labels": {"bye": "there"}}}
}


test_cache_match {
  matches_nsselector({"namespaceSelector": {"matchLabels": {"hi": "there"}}})
  	with data["{{.DataRoot}}"].cluster["v1"]["Namespace"]["my_namespace"] as {"metadata": {"labels": {"hi": "there"}}}
	with input.review.namespace as "my_namespace"
}

test_cache_no_match {
  not matches_nsselector({"namespaceSelector": {"matchLabels": {"bye": "there"}}})
  	with data["{{.DataRoot}}"].cluster["v1"]["Namespace"]["my_namespace"] as {"metadata": {"labels": {"hi": "there"}}}
	with input.review.namespace as "my_namespace"
}
