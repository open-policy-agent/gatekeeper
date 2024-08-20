#!/bin/bash

. ../../third_party/demo-magic/demo-magic.sh

clear

p "To ensure the ValidatingAdmissionPolicy feature is available for the cluster"

pe "kubectl api-resources | grep ValidatingAdmissionPolicy"

p "Deploy the constraint template"

pe "kubectl apply -f k8srequiredlabels_template.yaml"

p "View Constraint template to see the K8sNativeValidation engine and CEL rules are added"

pe "cat k8srequiredlabels_template.yaml"

pe "kubectl apply -f owner_must_be_provided.yaml"

pe "cat owner_must_be_provided.yaml"

p "Let's test the policy"

pe "kubectl create ns test"

p "Note the namespace was blocked by the Gatekeeper webhook as evaluated by the new CEL rules"

p "Now let's see how the ValidatingAdmissionPolicy feature works. First, let's check the --default-create-vap-binding-for-constraints flag is set to true to ensure ValidatingAdmissionPolicyBinding resources can be generated"

pe "kubectl get deploy gatekeeper-controller-manager -n gatekeeper-system -oyaml | grep default-create-vap-binding-for-constraints"

p "Let's update the constraint template to generate the ValidatingAdmissionPolicy and ValidatingAdmissionPolicyBinding resources"

pe "kubectl apply -f k8srequiredlabels_template_usevap.yaml"

pe "cat k8srequiredlabels_template_usevap.yaml"

p ""

p "Notice generateVAP: true is added to the source part of the constraint template to generate ValidatingAdmissionPolicy resources. And since --default-create-vap-binding-for-constraints is set to true, ValidatingAdmissionPolicyBinding is generated from the constraint resource. Let's see what the generated resources look like"

pe "kubectl get ValidatingAdmissionPolicy"

pe "kubectl get ValidatingAdmissionPolicyBinding"

p "Let's test the policy"

pe "kubectl create ns test"

p "Note the namespace was blocked by the ValidatingAdmissionPolicy admission controller"

p "THE END"

kubectl delete constrainttemplates --all
