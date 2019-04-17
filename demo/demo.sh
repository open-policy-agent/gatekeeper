#!/bin/bash

. ../third_party/demo-magic/demo-magic.sh

clear

pe "kubectl apply -f sync.yaml"

pe "kubectl create ns no-label"

pe "cat k8srequiredlabels_template.yaml"

pe "kubectl apply -f k8srequiredlabels_template.yaml"

pe "cat all_ns_must_have_gatekeeper.yaml"

pe "kubectl apply -f all_ns_must_have_gatekeeper.yaml"

pe "kubectl apply -f bad_ns.yaml"

pe "cat good_ns.yaml"

pe "kubectl apply -f good_ns.yaml"

pe "cat k8suniquelabels_template.yaml"

pe "kubectl apply -f k8suniquelabels_template.yaml"

pe "kubectl apply -f all_ns_gatekeeper_label_unique.yaml"

pe "cat no_dupe_ns.yaml"

pe "kubectl apply -f no_dupe_ns.yaml"

pe "cat no_dupe_ns_2.yaml"

pe "kubectl apply -f no_dupe_ns_2.yaml"

pe "kubectl get k8srequiredlabels ns-must-have-gk -o yaml"

p "THE END"

kubectl delete -f all_ns_gatekeeper_label_unique.yaml
kubectl delete -f all_ns_must_have_gatekeeper.yaml
kubectl delete -f k8suniquelabels_template.yaml
kubectl delete -f k8srequiredlabels_template.yaml
kubectl delete -f no_dupe_ns.yaml
kubectl delete -f good_ns.yaml
kubectl delete ns no-label

