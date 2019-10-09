package k8shttpsonly

violation[{"msg": msg}] {
  ingress = input.review.object
  not https_complete(ingress)
  msg := sprintf("Ingress should be https. tls configuration and allow-http=false annotation are required for %v", [ingress.metadata.name])
}

https_complete(ingress) = true {
  ingress.spec["tls"]
  count(ingress.spec.tls) > 0
  ingress.metadata.annotations["kubernetes.io/ingress.allow-http"] == "false"
}
