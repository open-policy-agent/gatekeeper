package k8suniqueingresshost

test_no_data {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1))}
    results := violation with input as input
    count(results) == 0
}
test_identical {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1))}
    inv := inventory_data([ingress("my-ingress", "prod", my_rules1)])
            trace(sprintf("%v", [inv]))

    results := violation with input as input with data.inventory as inv
                trace(sprintf("%v", [results]))

    count(results) == 0
}
test_collision {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1))}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules1)])
    results := violation with input as input with data.inventory as inv
    count(results) == 1
}
test_collision_with_multiple {
    input := {"review": review(ingress("my-ingress", "prod", my_rules3))}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules1), ingress("my-ingress1", "prod2", my_rules2)])
    results := violation with input as input with data.inventory as inv
    count(results) == 2
}
test_no_collision {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1))}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules2)])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}
test_no_collision_with_multiple {
    input := {"review": review(ingress("my-ingress", "prod", my_rules4))}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules1), ingress("my-ingress", "prod3", my_rules2)])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}



review(ing) = output {
  output = {
    "kind": {
      "kind": "Ingress",
      "version": "v1beta1",
      "group": "extension",
    },
    "namespace": ing.metadata.namespace,
    "name": ing.metadata.name,
    "object": ing,
  }
}

my_rule(host) = {
  "host": host, 
  "http": {
    "paths": [{"backend": {"serviceName": "nginx", "servicePort": 80}}]
  },
}

my_rules1 = [
    my_rule("a.abc.com")
]
my_rules2 = [
    my_rule("a1.abc.com")
]

my_rules3 = [
    my_rule("a.abc.com"),
    my_rule("a1.abc.com")
]

my_rules4 = [
    my_rule("a2.abc.com"),
    my_rule("a3.abc.com")
]


ingress(name, ns, rules) = out {
  out = {
    "kind": "Ingress",
    "apiVersion": "extension/v1beta1",
    "metadata": {
      "name": name,
      "namespace": ns,
    },
    "spec": {
      "rules": rules,
    },
  }
}

inventory_data(ingresses) = out {
  namespaces := {ns | ns = ingresses[_].metadata.namespace}
  out = {
    "namespace": {
      ns: {
        "extensions/v1beta1": {
          "Ingress": flatten_by_name(ingresses, ns)
        }
      } | ns := namespaces[_]
    }
  }
}

flatten_by_name(ingresses, ns) = out {
  out = {o.metadata.name: o | o = ingresses[_]; o.metadata.namespace = ns}
}