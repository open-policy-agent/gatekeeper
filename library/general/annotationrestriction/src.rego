package requiredannotations

violation[{"msg": msg}] {
  value := input.request.object.metadata.annotations[key]
  expected := input.parameters.annotations[_]
  expected.key == key
  expected.desiredValue != value
  msg := sprintf("Annotation <%v: %v> does not equal allowed value: %v", [key, value, expected.desiredValue])
}

violation[{"msg": msg, "details": {}}] {
  provided := input.request.object.metadata.annotations[key]
  required := {annotation | annotation := input.parameters.annotations[_].key}
  missing := required - provided
  count(missing) > 0
  msg := sprintf("You must provide annotations: '%v'.", [missing])
}
