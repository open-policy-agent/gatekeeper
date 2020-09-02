package k8simagedigests

test_input_allowed_container {
    input := { "review": input_review(input_container_allowed) }
    results := violation with input as input
    count(results) == 0
}
test_input_allowed_container_with_tag_and_digest {
    input := { "review": input_review(input_container_allowed_with_tag) }
    results := violation with input as input
    count(results) == 0
}
test_input_allowed_containers_with_registered_algorithms {
    input := { "review": input_review(input_container_allowed_registered_algorithms) }
    results := violation with input as input
    count(results) == 0
}
test_input_allowed_containers_with_unregistered_algorithms {
    input := { "review": input_review(input_container_allowed_unregistered_algorithms) }
    results := violation with input as input
    count(results) == 0
}
test_input_denied_container {
    input := { "review": input_review(input_container_denied) }
    results := violation with input as input
    count(results) == 1
}
test_input_denied_dual_container {
    input := { "review": input_review(input_container_dual_denied) }
    results := violation with input as input
    count(results) == 2
}
test_input_denied_mixed_container {
    input := { "review": input_review(array.concat(input_container_allowed, input_container_denied)) }
    results := violation with input as input
    count(results) == 1
}

# init containers
test_input_init_allowed_container {
    input := { "review": input_init_review(input_container_allowed) }
    results := violation with input as input
    count(results) == 0
}
test_input_init_allowed_container_with_tag_and_digest {
    input := { "review": input_init_review(input_container_allowed_with_tag) }
    results := violation with input as input
    count(results) == 0
}
test_input_init_allowed_containers_with_registered_algorithms {
    input := { "review": input_init_review(input_container_allowed_registered_algorithms) }
    results := violation with input as input
    count(results) == 0
}
test_input_init_allowed_containers_with_unregistered_algorithms {
    input := { "review": input_init_review(input_container_allowed_unregistered_algorithms) }
    results := violation with input as input
    count(results) == 0
}
test_input_init_denied_container {
    input := { "review": input_init_review(input_container_denied) }
    results := violation with input as input
    count(results) == 1
}
test_input_init_denied_dual_container {
    input := { "review": input_init_review(input_container_dual_denied) }
    results := violation with input as input
    count(results) == 2
}
test_input_init_denied_mixed_container {
    input := { "review": input_init_review(array.concat(input_container_allowed, input_container_denied)) }
    results := violation with input as input
    count(results) == 1
}

input_review(containers) = output {
    output = {
      "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": containers,
        }
      }
     }
}

input_init_review(containers) = output {
    output = {
      "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "initContainers": containers,
        }
      }
     }
}

input_container_allowed = [{
    "name": "image-with-sha256",
    "image": "allowed/nginx@sha256:b0ad43f7ee5edbc0effbc14645ae7055e21bc1973aee5150745632a24a752661",
}]

input_container_allowed_with_tag = [{
    "name": "image-with-tag-and-sha256",
    "image": "allowed/nginx:1.19.2@sha256:b0ad43f7ee5edbc0effbc14645ae7055e21bc1973aee5150745632a24a752661",
}]

input_container_allowed_registered_algorithms = [{
    "name": "image-with-sha256",
    "image": "allowed/nginx@sha256:b0ad43f7ee5edbc0effbc14645ae7055e21bc1973aee5150745632a24a752661",
},
{
    "name": "image-with-sha512",
    "image": "allowed/nginx@sha512:4b8bea0e757264767b373ba4963f4e972c428f42bf5335c2abb2e7f15a1b07c8aeddd2e62cacd0f2d669f49d8d975d952031e3041371639ec84baab400ab4802",
}]

input_container_allowed_unregistered_algorithms = [{
    "name": "unreg-multihash",
    "image": "allowed/unreg@multihash+base58:QmRZxt2b1FVZPNqd8hsiykDL3TdBDeTSPX9Kv46HmX4Gx8",
},
{
    "name": "unreg-sha256-with-base64url",
    "image": "allowed/unreg@sha256+b64u:LCa0a2j_xo_5m0U8HTBBNBNCLXBkg7-g-YpeiGJm564",
}]

input_container_denied = [{
    "name": "nginx",
    "image": "denied/nginx:1.19.2",
}]

input_container_dual_denied = [{
    "name": "nginx",
    "image": "denied/nginx:1.19.2",
},
{
    "name": "other",
    "image": "denied/other",
}]
