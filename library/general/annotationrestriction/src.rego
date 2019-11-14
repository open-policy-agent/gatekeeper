package requiredannotations

violation[{"msg": msg, "details": {}}] {
    #Retrieve the reference to the annotation you are looking for,
    #compare it to the value you specified, denies if they do not match.
    not input.review.object.metadata.annotations[input.parameters.annotationKey] == input.parameters.annotationValue
    msg := sprintf("Create / update of <%v> blocked - Annotation key <%v> must have value '%v'.", [input.review.object.kind, input.parameters.annotationKey, input.parameters.annotationValue])
}