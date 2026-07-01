package scaffoldpolicy

violation[{"msg": "scaffold violation"}] {
  input.review.object.metadata.name != "allowed"
}
