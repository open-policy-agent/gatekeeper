package k8sblocknodeport

test_block_node_port {
  input := {
    "review": {
      "kind": {"kind": "Service"},
      "object": {
        "spec": {
          "type": "NodePort"
        },
        "ports": {
                "port": 80,
                "targetPort": 80,
                "nodePort": 30007
            }
        }
      }
  }
  result := violation with input as input
  count(result) == 1
}
test_allow_other_service_types {
  input := {
    "review": {
      "kind": {"kind": "Service"},
      "object": {
        "spec": {
          "type": "LoadBalancer"
        },
        "ports": {
                "protocol": "TCP",
                "port": 80,
                "targetPort": 9376,
            }
        }
      }
  }
  result := violation with input as input
  count(result) == 0
}
