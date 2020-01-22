#!/bin/bash
scriptdir="$(dirname "$0")"
cd "$scriptdir"
cp ./../../deploy/gatekeeper.yaml ${PWD}/helm-modifications/_temp.yaml
kustomize build helm-modifications -o templates/gatekeeper.yaml
sed -i -E "s/HELMSUBST_DEPLOYMENT_CONTAINER_RESOURCES/\
\n{{ toYaml .Values.resources | indent 10 }}/" templates/gatekeeper.yaml
sed -i -E "s/HELMSUBST_DEPLOYMENT_POD_SCHEDULING/\
\n{{ toYaml .Values.nodeSelector | indent 8 }}\
\n      affinity:\
\n{{ toYaml .Values.affinity | indent 8 }}\
\n      tolerations:\
\n{{ toYaml .Values.tolerations | indent 8 }}/" templates/gatekeeper.yaml
sed -i "s/HELMSUBST_DEPLOYMENT_REPLICAS/{{ .Values.replicas }}/g" templates/gatekeeper.yaml
rm ./helm-modifications/_temp.yaml
echo "Helm template created under '$PWD/templates'"
