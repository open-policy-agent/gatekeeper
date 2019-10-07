package k8suniqueingresshostnames

test_no_data {
    input := {"review": review(ingress("my-ingress", "prod", "my-hostname"))}
    results := violation with input as input
    count(results) == 0
}
test_identical {
    input := {"review": review(ingress("my-ingress", "prod", "my-hostname"))}
    inv := tmp_data([ingress("my-ingress", "prod", "my-hostname")])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}
test_collision {
    input := {"review": review(ingress("my-ingress", "prod", "my-hostname"))}
    inv := tmp_data([ingress("my-ingress", "prod2", "my-hostname")])
    results := violation with input as input with data.inventory as inv
    count(results) == 1
}
test_collision_with_multiple {
    input := {"review": review(ingress("my-ingress", "prod", "my-hostname"))}
    inv := tmp_data([ingress("my-ingress", "prod2", "my-hostname"), ingress("my-ingress", "prod3", "my-hostname")])
    results := violation with input as input with data.inventory as inv
    count(results) == 2
}
test_no_collision {
    input := {"review": review(ingress("my-ingress", "prod", "my-hostname"))}
    inv := tmp_data([ingress("my-ingress", "prod2", "other-hostname")])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}
test_no_collision_with_multiple {
    input := {"review": review(ingress("my-ingress", "prod", "my-hostname"))}
    inv := tmp_data([ingress("my-ingress", "prod2", "unique-hostname-1"), ingress("my-ingress", "prod3", "unique-hostname-2")])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}
test_mixed_collision_with_multiple {
    input := {"review": review(ingress("my-ingress", "prod", "my-hostname"))}
    inv := tmp_data([ingress("my-ingress", "prod2", "my-hostname"), ingress("my-ingress", "prod3", "other-hostname")])
    results := violation with input as input with data.inventory as inv
    count(results) == 1
}



review(ingress) = output {
  output = {
    "kind": {
      "kind": "Ingress",
      "version": "v1",
      "group": "",
    },
    "namespace": ingress.metadata.namespace,
    "name": ingress.metadata.name,
    "object": ingress,
  }
}

ingress(name, ns, hostname) = out {
  out = {
    "kind": "Ingress",
    "apiVersion": "v1",
    "metadata": {
      "name": name,
      "namespace": ns,
    },
    "spec": {"rules": [{"host": hostname}]}
  }
}


tmp_data(ingresses) = out {
  namespaces := {ns | ns = ingresses[_].metadata.namespace}
  out = {
    "namespace": {
      ns: {
        "v1": {
          "Ingress": flatten_by_name(ingresses, ns)
        }
      } | ns := namespaces[_]
    }
  }
}

flatten_by_name(ingresses, ns) = out {
  out = {o.metadata.name: o | o = ingresses[_]; o.metadata.namespace = ns}
}
