    package admission

    import data.k8s.matches
    
    ##############################################################################
    #
    # Policy : Ingress hostnames must be unique across Namespaces.
    #
    # This policy shows how you can express a pair-wise search. In this case, there
    # is a violation if any two ingresses in different namespaces. Note, you can
    # query OPA to determine whether a single Ingress violates the policy (in which
    # case the cost is linear with the # of Ingresses) or you can query for the set
    # of all Ingresses th violate the policy (in which case the cost is (# of
    # Ingresses)^2.)
    #
    ##############################################################################

    deny[{
        "id": "ingress-conflict",
        "resource": {"kind": "ingresses", "namespace": namespace, "name": name},
        "message": "ingress host conflicts with an existing ingress",
    }] {
        matches[["ingresses", namespace, name, matched_ingress]]
        matches[["ingresses", other_ns, other_name, other_ingress]]
        namespace != other_ns
        other_ingress.spec.rules[_].host == matched_ingress.spec.rules[_].host
    }