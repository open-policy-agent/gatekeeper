#!/bin/bash
scriptdir="$(dirname "$0")"
cd "$scriptdir"
cp ./../../deploy/gatekeeper.yaml ${PWD}/helm-modifications/_temp.yaml
kustomize build helm-modifications -o templates/gatekeeper.yaml
sed -i -E "s/HELMSUBST_DEPLOYMENT_RESOURCES/\
\n{{ toYaml .Values.resources | indent 12 }}\
\n    {{- with .Values.nodeSelector }}\
\n      nodeSelector:\
\n{{ toYaml . | indent 8 }}\
\n    {{- end }}\
\n    {{- with .Values.affinity }}\
\n      affinity:\
\n{{ toYaml . | indent 8 }}\
\n    {{- end }}\
\n    {{- with .Values.tolerations }}\
\n      tolerations:\
\n{{ toYaml . | indent 8 }}\
\n    {{- end }}/" templates/gatekeeper.yaml
sed -i "s/HELMSUBST_VALUES_REPLICAS_PLACEHOLDER/{{ .Values.Replicas }}/g" templates/gatekeeper.yaml
rm ./helm-modifications/_temp.yaml
echo "Helm template created under 'chart/gatekeeper-operator/templates'"
