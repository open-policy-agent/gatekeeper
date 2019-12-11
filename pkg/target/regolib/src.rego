package target

##################
# Required Hooks #
##################

autoreject_review[rejection] {
  constraint := data["{{.ConstraintsRoot}}"][_][_]
  spec := get_default(constraint, "spec", {})
  match := get_default(spec, "match", {})
  has_field(match, "namespaceSelector")
  not data["{{.DataRoot}}"].cluster["v1"]["Namespace"][input.review.namespace]
  not input.review._unstable.namespace
  not input.review.namespace == ""
  rejection := {
    "msg": "Namespace is not cached in OPA.",
    "details": {},
    "constraint": constraint,
  }
}

matching_constraints[constraint] {
  constraint := data["{{.ConstraintsRoot}}"][_][_]
  spec := get_default(constraint, "spec", {})
  match := get_default(spec, "match", {})

  any_kind_selector_matches(match)

  matches_namespaces(match)

  matches_nsselector(match)

  label_selector := get_default(match, "labelSelector", {})
  obj := get_default(input.review, "object", {})
  metadata := get_default(obj, "metadata", {})
  labels := get_default(metadata, "labels", {})
  matches_label_selector(label_selector, labels)
}

# Namespace-scoped objects
matching_reviews_and_constraints[[review, constraint]] {
  obj = data["{{.DataRoot}}"].namespace[namespace][api_version][kind][name]
  r := make_review(obj, api_version, kind, name)
  review := add_field(r, "namespace", namespace)
  matching_constraints[constraint] with input as {"review": review}
}

# Cluster-scoped objects
matching_reviews_and_constraints[[review, constraint]] {
  obj = data["{{.DataRoot}}"].cluster[api_version][kind][name]
  review = make_review(obj, api_version, kind, name)
  matching_constraints[constraint] with input as {"review": review}
}

make_review(obj, api_version, kind, name) = review {
  [group, version] := make_group_version(api_version)
  review := {
    "kind": {"group": group, "version": version, "kind": kind},
    "name": name,
    "object": obj
  }
}

########
# Util #
########

make_group_version(api_version) = [group, version] {
  contains(api_version, "/")
  [group, version] := split(api_version, "/")
}

make_group_version(api_version) = [group, version] {
  not contains(api_version, "/")
  group := ""
  version := api_version
}

add_field(object, key, value) = ret {
  keys := {k | object[k]}
  allKeys = keys | {key}
  ret := {k: v | v = get_default(object, k, value); allKeys[k]}
}

# has_field returns whether an object has a field
has_field(object, field) = true {
  object[field]
}

# False is a tricky special case, as false responses would create an undefined document unless
# they are explicitly tested for
has_field(object, field) = true {
  object[field] == false
}

has_field(object, field) = false {
  not object[field]
  not object[field] == false
}

# get_default returns the value of an object's field or the provided default value.
# It avoids creating an undefined state when trying to access an object attribute that does
# not exist
get_default(object, field, _default) = output {
  has_field(object, field)
  output = object[field]
}

get_default(object, field, _default) = output {
  has_field(object, field) == false
  output = _default
}

#######################
# Kind Selector Logic #
#######################

any_kind_selector_matches(match) {
  kind_selectors := get_default(match, "kinds", [{"apiGroups": ["*"], "kinds": ["*"]}])
  ks := kind_selectors[_]
  kind_selector_matches(ks)
}

kind_selector_matches(ks) {
  group_matches(ks)
  kind_matches(ks)
}

group_matches(ks) {
  ks.apiGroups[_] == "*"
}

group_matches(ks) {
  ks.apiGroups[_] == input.review.kind.group
}

kind_matches(ks) {
  ks.kinds[_] == "*"
}

kind_matches(ks) {
  ks.kinds[_] == input.review.kind.kind
}

########################
# Label Selector Logic #
########################

# match_expression_violated checks to see if a match expression is violated.
match_expression_violated("In", labels, key, values) = true {
  has_field(labels, key) == false
}

match_expression_violated("In", labels, key, values) = true {
  # values array must be non-empty for rule to be valid
  count(values) > 0
  valueSet := {v | v = values[_]}
  count({labels[key]} - valueSet) != 0
}

# No need to check if labels has the key, because a missing key is automatic non-violation
match_expression_violated("NotIn", labels, key, values) = true {
  # values array must be non-empty for rule to be valid
  count(values) > 0
  valueSet := {v | v = values[_]}
  count({labels[key]} - valueSet) == 0
}

match_expression_violated("Exists", labels, key, values) = true {
  has_field(labels, key) == false
}

match_expression_violated("DoesNotExist", labels, key, values) = true {
  has_field(labels, key) == true
}


# Checks to see if a kubernetes LabelSelector matches a given set of labels
# A non-existent selector or labels should be represented by an empty object ("{}")
matches_label_selector(selector, labels) {
  keys := {key | labels[key]}
  matchLabels := get_default(selector, "matchLabels", {})
  satisfiedMatchLabels := {key | matchLabels[key] == labels[key]}
  count(satisfiedMatchLabels) == count(matchLabels)

  matchExpressions := get_default(selector, "matchExpressions", [])

  mismatches := {failure | failure = true; failure = match_expression_violated(
    matchExpressions[i]["operator"],
    labels,
    matchExpressions[i]["key"],
    get_default(matchExpressions[i], "values", []))}

  any(mismatches) == false
}

############################
# Namespace Selector Logic #
############################

get_ns[out] {
  out := input.review._unstable.namespace
}

get_ns[out] {
  not input.review._unstable.namespace
  out := data["{{.DataRoot}}"].cluster["v1"]["Namespace"][input.review.namespace]
}

matches_namespaces(match) {
  not has_field(match, "namespaces")
}

matches_namespaces(match) {
  has_field(match, "namespaces")
  ns := {n | n = match.namespaces[_]}
  count({input.review.namespace} - ns) == 0
}

matches_nsselector(match) {
  not has_field(match, "namespaceSelector")
}

matches_nsselector(match) {
  has_field(match, "namespaceSelector")
  get_ns[ns]
  matches_namespace_selector(match, ns)
}


# Checks to see if a kubernetes NamespaceSelector matches a namespace with a given set of labels
# A non-existent selector or labels should be represented by an empty object ("{}")
matches_namespace_selector(match, ns) {
  metadata := get_default(ns, "metadata", {})
  nslabels := get_default(metadata, "labels", {})
  namespace_selector := get_default(match, "namespaceSelector", {})
  matches_label_selector(namespace_selector, nslabels)
}
