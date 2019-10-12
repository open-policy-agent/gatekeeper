package k8suniqueingresshost

test_no_data {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1, "extensions/v1beta1"), "extensions")}
    results := violation with input as input
    count(results) == 0
}
test_identical {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1, "extensions/v1beta1"), "extensions")}
    inv := inventory_data([ingress("my-ingress", "prod", my_rules1, "extensions/v1beta1")])
            trace(sprintf("%v", [inv]))

    results := violation with input as input with data.inventory as inv
                trace(sprintf("%v", [results]))

    count(results) == 0
}
test_collision {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1, "extensions/v1beta1"), "extensions")}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules1, "extensions/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 1
}
test_collision_with_multiple {
    input := {"review": review(ingress("my-ingress", "prod", my_rules3, "extensions/v1beta1"), "extensions")}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules1, "extensions/v1beta1"), ingress("my-ingress1", "prod2", my_rules2, "extensions/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 2
}
test_no_collision {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1, "extensions/v1beta1"), "extensions")}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules2, "extensions/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}
test_no_collision_with_multiple {
    input := {"review": review(ingress("my-ingress", "prod", my_rules4, "extensions/v1beta1"), "extensions")}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules1, "extensions/v1beta1"), ingress("my-ingress", "prod3", my_rules2, "extensions/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}
test_no_collision_with_multiple_apis {
    input := {"review": review(ingress("my-ingress", "prod", my_rules4, "networking.k8s.io/v1beta1"), "networking.k8s.io")}
    inv := inventory_data2([ingress("my-ingress", "prod2", my_rules1, "networking.k8s.io/v1beta1"), ingress("my-ingress", "prod3", my_rules2, "networking.k8s.io/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}
test_collision_with_multiple_apis {
    input := {"review": review(ingress("my-ingress", "prod", my_rules3, "networking.k8s.io/v1beta1"), "networking.k8s.io")}
    inv := inventory_data2([ingress("my-ingress", "prod2", my_rules1, "networking.k8s.io/v1beta1"), ingress("my-ingress", "prod3", my_rules2, "networking.k8s.io/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 2
}
test_no_collision_with_multiple_bad_review_apis {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1, "app/v1beta1"), "app")}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules1, "extensions/v1beta1"), ingress("my-ingress", "prod3", my_rules2, "extensions/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}
test_no_collision_with_multiple_bad_review_apis2 {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1, "test.extensions/v1beta1"), "test.extensions")}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules1, "extensions/v1beta1"), ingress("my-ingress", "prod3", my_rules2, "extensions/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}
test_collision_with_multiple_apis_mixed {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1, "networking.k8s.io/v1beta1"), "networking.k8s.io")}
    inv := inventory_data([ingress("my-ingress", "prod2", my_rules1, "extensions/v1beta1"), ingress("my-ingress", "prod3", my_rules2, "extensions/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 1
}
test_no_collision_with_multiple_apis_slash {
    input := {"review": review(ingress("my-ingress", "prod", my_rules1, "networking.k8s.io/v1beta1"), "networking.k8s.io")}
    inv := inventory_data1([ingress("my-ingress", "prod2", my_rules1, "extensions.something.io/v1beta1"), ingress("my-ingress", "prod3", my_rules2, "extensions.something.io/v1beta1")])
    results := violation with input as input with data.inventory as inv
    count(results) == 0
}



review(ing, group) = output {
  output = {
    "kind": {
      "kind": "Ingress",
      "version": "v1beta1",
      "group": group,
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


ingress(name, ns, rules, apiversion) = out {
  out = {
    "kind": "Ingress",
    "apiVersion": apiversion,
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

inventory_data1(ingresses) = out {
  namespaces := {ns | ns = ingresses[_].metadata.namespace}
  out = {
    "namespace": {
      ns: {
        "extensions.something.io/v1beta1": {
          "Ingress": flatten_by_name(ingresses, ns)
        }
      } | ns := namespaces[_]
    }
  }
}

inventory_data2(ingresses) = out {
  namespaces := {ns | ns = ingresses[_].metadata.namespace}
  out = {
    "namespace": {
      ns: {
        "networking.k8s.io/v1beta1": {
          "Ingress": flatten_by_name(ingresses, ns)
        }
      } | ns := namespaces[_]
    }
  }
}

flatten_by_name(ingresses, ns) = out {
  out = {o.metadata.name: o | o = ingresses[_]; o.metadata.namespace = ns}
}
