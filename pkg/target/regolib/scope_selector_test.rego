package target

test_match_empty_with_namespaced {
  matches_scope({})  with input.review as {"namespace": "foo"}
}

test_match_empty_with_cluster_scoped {
  matches_scope({})  with input.review as {}
}

test_match_any_with_namespaced {
  matches_scope({"scope": "*"}) with input.review as {"namespace": "foo"}
}

test_match_any_with_cluster_scoped {
  matches_scope({"scope": "*"})  with input.review as {}
}

test_match_namespaced_with_namespaced {
  matches_scope({"scope": "Namespaced"}) with input.review as {"namespace": "foo"}
}

test_match_namespaced_with_cluster_scoped {
  not matches_scope({"scope": "Namespaced"}) with input.review as {}
}

test_match_cluster_with_namespaced {
  not matches_scope({"scope": "Cluster"}) with input.review as {"namespace": "foo"}
}

test_match_cluster_with_cluster_scoped {
  matches_scope({"scope": "Cluster"}) with input.review as {}
}
