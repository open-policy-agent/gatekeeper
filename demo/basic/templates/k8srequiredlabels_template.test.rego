package k8srequiredlabels

object := {"object":{"kind":"Pod","metadata":{"name":"a","namespace":"b","labels":{"foo":"a"}}}}

test_ignores_good_pods {
  count(violation) == 0 with input as {"review":object,"parameters":{"labels":["foo"]}}
}

test_blocks_bad_pods {
  count(violation) == 1 with input as {"review":object,"parameters":{"labels":["bar"]}}
}

test_blocks_bad_namespaceless_kinds {
  object := {"object":{"kind":"Namespace","metadata":{"name":"a","labels":{"foo":"a"}}}}
  count(violation) == 1 with input as {"review":object,"parameters":{"labels":["bar"]}}
}
