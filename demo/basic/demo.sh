#!/bin/bash

. ../../third_party/demo-magic/demo-magic.sh

clear

pe "kubectl apply -f sync.yaml"

pe "kubectl create ns no-label"

pe "cat templates/k8srequiredlabels_template.yaml"

pe "kubectl apply -f templates/k8srequiredlabels_template.yaml"

pe "cat constraints/all_ns_must_have_gatekeeper.yaml"

pe "kubectl apply -f constraints/all_ns_must_have_gatekeeper.yaml"

pe "kubectl apply -f bad/bad_ns.yaml"

pe "cat good/good_ns.yaml"

pe "kubectl apply -f good/good_ns.yaml"

pe "cat templates/k8suniquelabels_template.yaml"

pe "kubectl apply -f templates/k8suniquelabels_template.yaml"

pe "kubectl apply -f constraints/all_ns_gatekeeper_label_unique.yaml"

pe "cat good/no_dupe_ns.yaml"

pe "kubectl apply -f good/no_dupe_ns.yaml"

pe "cat bad/no_dupe_ns_2.yaml"

pe "kubectl apply -f bad/no_dupe_ns_2.yaml"

pe "kubectl get k8srequiredlabels ns-must-have-gk -o yaml"

p "THE END"

kubectl delete -f constraints
kubectl delete -f templates
kubectl delete -f good
kubectl delete ns no-label
kubectl delete -f sync.yaml
