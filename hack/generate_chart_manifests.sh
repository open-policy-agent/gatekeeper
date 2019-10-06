#!/bin/bash

CHART_PATH=chart/gatekeeper-operator
CONFIG_PATH=../config

# templates/crds.configs.yaml update
awk 'FNR==1 && NR!=1 {print "---"}{print}' ${CONFIG_PATH}/crds/*.yaml > ${CHART_PATH}/templates/_crds.yaml
yq m -d'*' -i ${CHART_PATH}/templates/_crds.yaml chart-metadata.yaml
yq w -d'*' -i ${CHART_PATH}/templates/_crds.yaml 'metadata.annotations[helm.sh/hook]' crd-install
yq w -d'*' -i ${CHART_PATH}/templates/_crds.yaml 'metadata.annotations[helm.sh/hook-delete-policy]' before-hook-creation
yq d -d'*' -i ${CHART_PATH}/templates/_crds.yaml metadata.creationTimestamp
yq d -d'*' -i ${CHART_PATH}/templates/_crds.yaml status
yq d -d'*' -i ${CHART_PATH}/templates/_crds.yaml spec.validation

# add shortName to CRD until https://github.com/kubernetes-sigs/kubebuilder/issues/404 is solved
yq w -d0 -i ${CHART_PATH}/templates/_crds.yaml 'spec.names.shortNames[+]' config

echo '{{- if (and .Values.installCRDs (not (.Capabilities.APIVersions.Has "config.gatekeeper.sh/v1alpha1"))) }}' > ${CHART_PATH}/templates/crds.configs.yaml
cat ${CHART_PATH}/templates/_crds.yaml >> ${CHART_PATH}/templates/crds.configs.yaml
echo '{{- end }}' >> ${CHART_PATH}/templates/crds.configs.yaml
rm ${CHART_PATH}/templates/_crds.yaml

# templates/crds.constraints.yaml update
awk 'FNR==1 && NR!=1 {print "---"}{print}' ../vendor/github.com/open-policy-agent/frameworks/constraint/config/crds/templates_v1beta1_constrainttemplate.yaml > ${CHART_PATH}/templates/_crds.yaml
yq m -d'*' -i ${CHART_PATH}/templates/_crds.yaml chart-metadata.yaml
yq w -d'*' -i ${CHART_PATH}/templates/_crds.yaml 'metadata.annotations[helm.sh/hook]' crd-install
yq w -d'*' -i ${CHART_PATH}/templates/_crds.yaml 'metadata.annotations[helm.sh/hook-delete-policy]' before-hook-creation
yq d -d'*' -i ${CHART_PATH}/templates/_crds.yaml metadata.creationTimestamp
yq d -d'*' -i ${CHART_PATH}/templates/_crds.yaml status
yq d -d'*' -i ${CHART_PATH}/templates/_crds.yaml spec.validation

# add shortName to CRD until https://github.com/kubernetes-sigs/kubebuilder/issues/404 is solved
yq w -d0 -i ${CHART_PATH}/templates/_crds.yaml 'spec.names.shortNames[+]' constraints

echo '{{- if (and .Values.installCRDs (not (.Capabilities.APIVersions.Has "templates.gatekeeper.sh/v1beta1"))) }}'> ${CHART_PATH}/templates/crds.constraints.yaml
cat ${CHART_PATH}/templates/_crds.yaml >> ${CHART_PATH}/templates/crds.constraints.yaml
echo '{{- end }}' >> ${CHART_PATH}/templates/crds.constraints.yaml
rm ${CHART_PATH}/templates/_crds.yaml

# templates/rbac.yaml update
cp ${CONFIG_PATH}/rbac/rbac_role.yaml ${CHART_PATH}/templates/rbac.yaml
yq m -d'*' -i ${CHART_PATH}/templates/rbac.yaml chart-metadata.yaml
yq d -d'*' -i ${CHART_PATH}/templates/rbac.yaml metadata.creationTimestamp
yq w -d'*' -i ${CHART_PATH}/templates/rbac.yaml metadata.name '{{ template "gatekeeper-operator.fullname" . }}'
echo '{{- if .Values.rbac.create }}' > ${CHART_PATH}/templates/clusterrole.yaml
cat ${CHART_PATH}/templates/rbac.yaml >> ${CHART_PATH}/templates/clusterrole.yaml
echo '{{- end }}' >> ${CHART_PATH}/templates/clusterrole.yaml
rm ${CHART_PATH}/templates/rbac.yaml
