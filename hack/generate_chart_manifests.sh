#!/bin/bash

CHART_PATH=chart/gatekeeper-operator
CONFIG_PATH=../config

# templates/crds.configs.yaml update
echo "Updating templates/crds.configs.yaml"
awk 'FNR==1 && NR!=1 {print "---"}{print}' ${CONFIG_PATH}/crds/*.yaml > ${CHART_PATH}/crds.configs/_crds.yaml
cp chart-metadata.yaml ${CHART_PATH}/crds.configs/_chart-metadata.yaml
sed -i 's/METADATA_NAME_PLACEHOLDER/configs.config.gatekeeper.sh/g' ${CHART_PATH}/crds.configs/_chart-metadata.yaml
sed -i 's/API_VERSION_PLACEHOLDER/apiextensions.k8s.io\/v1beta1/g' ${CHART_PATH}/crds.configs/_chart-metadata.yaml
sed -i 's/KIND_PLACEHOLDER/CustomResourceDefinition/g' ${CHART_PATH}/crds.configs/_chart-metadata.yaml
kustomize build ${CHART_PATH}/crds.configs -o ${CHART_PATH}/templates/_crds.configs.yaml
echo '{{- if (and .Values.installCRDs (not (.Capabilities.APIVersions.Has "config.gatekeeper.sh/v1alpha1"))) }}' > ${CHART_PATH}/templates/crds.configs.yaml
cat ${CHART_PATH}/templates/_crds.configs.yaml >> ${CHART_PATH}/templates/crds.configs.yaml
echo '{{- end }}' >> ${CHART_PATH}/templates/crds.configs.yaml
sed -i '/SPEC_VALIDATION_REMOVE_PLACEHOLDER/d' ${CHART_PATH}/templates/crds.configs.yaml
rm ${CHART_PATH}/templates/_crds.configs.yaml
rm ${CHART_PATH}/crds.configs/_crds.yaml
rm ${CHART_PATH}/crds.configs/_chart-metadata.yaml

# templates/crds.constraints.yaml update
echo "Updating templates/crds.constraints.yaml"
awk 'FNR==1 && NR!=1 {print "---"}{print}' ../vendor/github.com/open-policy-agent/frameworks/constraint/config/crds/templates_v1beta1_constrainttemplate.yaml > ${CHART_PATH}/crds.constraints/_crds.yaml
cp chart-metadata.yaml ${CHART_PATH}/crds.constraints/_chart-metadata.yaml
sed -i 's/METADATA_NAME_PLACEHOLDER/constrainttemplates.templates.gatekeeper.sh/g' ${CHART_PATH}/crds.constraints/_chart-metadata.yaml
sed -i 's/API_VERSION_PLACEHOLDER/apiextensions.k8s.io\/v1beta1/g' ${CHART_PATH}/crds.constraints/_chart-metadata.yaml
sed -i 's/KIND_PLACEHOLDER/CustomResourceDefinition/g' ${CHART_PATH}/crds.constraints/_chart-metadata.yaml
kustomize build ${CHART_PATH}/crds.constraints -o ${CHART_PATH}/templates/_crds.constraints.yaml
echo '{{- if (and .Values.installCRDs (not (.Capabilities.APIVersions.Has "templates.gatekeeper.sh/v1beta1"))) }}'> ${CHART_PATH}/templates/crds.constraints.yaml
cat ${CHART_PATH}/templates/_crds.constraints.yaml >> ${CHART_PATH}/templates/crds.constraints.yaml
echo '{{- end }}' >> ${CHART_PATH}/templates/crds.constraints.yaml
sed -i '/SPEC_VALIDATION_REMOVE_PLACEHOLDER/d' ${CHART_PATH}/templates/crds.constraints.yaml
rm ${CHART_PATH}/templates/_crds.constraints.yaml
rm ${CHART_PATH}/crds.constraints/_crds.yaml
rm ${CHART_PATH}/crds.constraints/_chart-metadata.yaml

# templates/rbac.yaml update
echo "Updating templates/rbac.yaml"
cp ${CONFIG_PATH}/rbac/rbac_role.yaml ${CHART_PATH}/rbac/_rbac.yaml
GATEKEEPER_OPERATOR_NAME="'{{ template \"gatekeeper-operator.fullname\" . }}'"
sed -i "s/manager-role/${GATEKEEPER_OPERATOR_NAME}/g" ${CHART_PATH}/rbac/_rbac.yaml
cp chart-metadata.yaml ${CHART_PATH}/rbac/_chart-metadata.yaml
sed -i "s/METADATA_NAME_PLACEHOLDER/${GATEKEEPER_OPERATOR_NAME}/g" ${CHART_PATH}/rbac/_chart-metadata.yaml
sed -i 's/API_VERSION_PLACEHOLDER/rbac.authorization.k8s.io\/v1/g' ${CHART_PATH}/rbac/_chart-metadata.yaml
sed -i 's/KIND_PLACEHOLDER/ClusterRole/g' ${CHART_PATH}/rbac/_chart-metadata.yaml
kustomize build ${CHART_PATH}/rbac -o ${CHART_PATH}/templates/_rbac.yaml
echo '{{- if .Values.rbac.create }}' > ${CHART_PATH}/templates/clusterrole.yaml
cat ${CHART_PATH}/templates/_rbac.yaml >> ${CHART_PATH}/templates/clusterrole.yaml
echo '{{- end }}' >> ${CHART_PATH}/templates/clusterrole.yaml
rm ${CHART_PATH}/rbac/_rbac.yaml
rm ${CHART_PATH}/rbac/_chart-metadata.yaml
rm ${CHART_PATH}/templates/_rbac.yaml
