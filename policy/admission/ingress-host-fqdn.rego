    package admission

    import data.k8s.matches
    import data.kubernetes.namespaces
    
    ##############################################################################
    #
    # Policy : Ingress hostnames must be whitelisted on namespace.
    #
    # This policy shows how you can leverage context beyond an individual resource
    # to make decisions. In this case, the whitelist is stored on the Namespace
    # associated with the Ingress. To decide whether the Ingress hostname violates
    # our policy, we check if it matches of the whitelisted patterns stored on the
    # Namespace.
    #
    ##############################################################################
    deny[{
        "id": "ingress-host-fqdn",   # identifies type of violation
        "resource": {
            "kind": "ingresses",            # identifies kind of resource
            "namespace": namespace,         # identifies namespace of resource
            "name": name                    # identifies name of resource
        },
        "message": msg,                     # provides human-readable message to display
    }] {
        matches[["ingresses", namespace, name, matched_ingress]]
        host := matched_ingress.spec.rules[_].host
        valid_hosts := valid_ingress_hosts(namespace)
        not fqdn_matches_any(host, valid_hosts)
        msg := sprintf("invalid ingress host fqdn %q", [host])
    }
    valid_ingress_hosts(namespace) = {host |
        whitelist := namespaces[namespace].metadata.annotations["ingress-whitelist"]
        hosts := split(whitelist, ",")
        host := hosts[_]
    }

    fqdn_matches_any(str, patterns) {
        fqdn_matches(str, patterns[_])
    }

    fqdn_matches(str, pattern) {
        pattern_parts = split(pattern, ".")
        pattern_parts[0] = "*"
        str_parts = split(str, ".")
        n_pattern_parts = count(pattern_parts)
        n_str_parts = count(str_parts)
        suffix = trim(pattern, "*.")
        endswith(str, suffix)
    }

    fqdn_matches(str, pattern) {
        not contains(pattern, "*")
        str == pattern
    }