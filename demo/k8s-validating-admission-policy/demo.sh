#!/bin/bash

. ../../third_party/demo-magic/demo-magic.sh

clear

p "To ensure the ValidatingAdmissionPolicy feature is enabled for the cluster"

pe "kubectl api-resources | grep ValidatingAdmissionPolicy"

p "Deploy the constraint template"

pe "kubectl apply -f k8srequiredlabels_template.yaml"

p "View Constraint template to see a new engine and CEL rules are added"

pe "cat k8srequiredlabels_template.yaml"

pe "kubectl apply -f owner_must_be_provided.yaml"

pe "cat owner_must_be_provided.yaml"

p "Let's test the policy"

pe "kubectl create ns test"

p "Note the bad namespace was blocked by the Gatekeeper webhook as evaluated by the new CEL rules"

p "Now let's update the constraint template to generate the ValidatingAdmissionPolicy resources"

pe "kubectl apply -f k8srequiredlabels_template_usevap.yaml"

pe "cat k8srequiredlabels_template_usevap.yaml"

p "Notice the "gatekeeper.sh/use-vap": "yes" label was added to the template to generate ValidatingAdmissionPolicy resources. Let's see what the generated resources look like"

pe "kubectl get ValidatingAdmissionPolicy"

pe "kubectl get ValidatingAdmissionPolicyBinding"

p "Let's test the policy"

pe "kubectl create ns test"

p "Note the bad namespace was blocked by the ValidatingAdmissionPolicy admission controller"

p "THE END"

kubectl delete constrainttemplates --all
