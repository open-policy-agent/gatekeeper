#!/bin/bash

CHART_PATH=chart/gatekeeper-operator
CONFIG_PATH=../config

# templates/ns.yaml update
# echo "Updating templates/ns.yaml"
# cp ${CONFIG_PATH}/manager/manager.yaml ${CHART_PATH}/ns/_ns.yaml
# kustomize build ${CHART_PATH}/ns -o ${CHART_PATH}/templates/ns.yaml

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

# templates/clusterrole.yaml update
echo "Updating templates/clusterrole.yaml"
cp ${CONFIG_PATH}/rbac/rbac_role.yaml ${CHART_PATH}/clusterrole/_clusterrole.yaml
GATEKEEPER_OPERATOR_NAME="'{{ template \"gatekeeper-operator.fullname\" . }}'"
sed -i "s/manager-role/${GATEKEEPER_OPERATOR_NAME}/g" ${CHART_PATH}/clusterrole/_clusterrole.yaml
cp chart-metadata.yaml ${CHART_PATH}/clusterrole/_chart-metadata.yaml
sed -i "s/METADATA_NAME_PLACEHOLDER/${GATEKEEPER_OPERATOR_NAME}/g" ${CHART_PATH}/clusterrole/_chart-metadata.yaml
sed -i 's/API_VERSION_PLACEHOLDER/rbac.authorization.k8s.io\/v1/g' ${CHART_PATH}/clusterrole/_chart-metadata.yaml
sed -i 's/KIND_PLACEHOLDER/ClusterRole/g' ${CHART_PATH}/clusterrole/_chart-metadata.yaml
kustomize build ${CHART_PATH}/clusterrole -o ${CHART_PATH}/templates/_clusterrole.yaml
echo '{{- if .Values.rbac.create }}' > ${CHART_PATH}/templates/clusterrole.yaml
cat ${CHART_PATH}/templates/_clusterrole.yaml >> ${CHART_PATH}/templates/clusterrole.yaml
echo '{{- end }}' >> ${CHART_PATH}/templates/clusterrole.yaml
rm ${CHART_PATH}/clusterrole/_clusterrole.yaml
rm ${CHART_PATH}/clusterrole/_chart-metadata.yaml
rm ${CHART_PATH}/templates/_clusterrole.yaml

# templates/clusterrolebinding.yaml
echo "Updating templates/clusterrolebinding.yaml"
cp ${CONFIG_PATH}/rbac/rbac_role_binding.yaml ${CHART_PATH}/clusterrolebinding/_clusterrolebinding.yaml
GATEKEEPER_OPERATOR_ROLEBINDING_NAME="'{{ template \"gatekeeper-operator.fullname\" . }}binding'"
sed -i "s/manager-rolebinding/${GATEKEEPER_OPERATOR_ROLEBINDING_NAME}/g" ${CHART_PATH}/clusterrolebinding/_clusterrolebinding.yaml
sed -i "s/manager-role/${GATEKEEPER_OPERATOR_NAME}/g" ${CHART_PATH}/clusterrolebinding/_clusterrolebinding.yaml
cp chart-metadata.yaml ${CHART_PATH}/clusterrolebinding/_chart-metadata.yaml
sed -i "s/METADATA_NAME_PLACEHOLDER/${GATEKEEPER_OPERATOR_ROLEBINDING_NAME}/g" ${CHART_PATH}/clusterrolebinding/_chart-metadata.yaml
sed -i 's/API_VERSION_PLACEHOLDER/rbac.authorization.k8s.io\/v1/g' ${CHART_PATH}/clusterrolebinding/_chart-metadata.yaml
sed -i 's/KIND_PLACEHOLDER/ClusterRoleBinding/g' ${CHART_PATH}/clusterrolebinding/_chart-metadata.yaml
kustomize build ${CHART_PATH}/clusterrolebinding -o ${CHART_PATH}/templates/_clusterrolebinding.yaml
echo '{{- if .Values.rbac.create }}' > ${CHART_PATH}/templates/clusterrolebinding.yaml
cat ${CHART_PATH}/templates/_clusterrolebinding.yaml >> ${CHART_PATH}/templates/clusterrolebinding.yaml
echo '{{- end }}' >> ${CHART_PATH}/templates/clusterrolebinding.yaml
rm ${CHART_PATH}/clusterrolebinding/_clusterrolebinding.yaml
rm ${CHART_PATH}/clusterrolebinding/_chart-metadata.yaml
rm ${CHART_PATH}/templates/_clusterrolebinding.yaml