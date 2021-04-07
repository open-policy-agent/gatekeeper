#!/bin/bash

# Delete the Helm release secret but keeps the resources
# Updates the annotations so the resources can be imported by re-installed chart.


RELEASE_NAMESPACE=${RELEASE_NAMESPACE:-default}
RELEASE_NAME=${RELEASE_NAME:-gatekeeper}
NEW_NAMESPACE=${NEW_NAMESPACE:-gatekeeper-system}



kubectl -n ${RELEASE_NAMESPACE}  delete secrets --field-selector type=helm.sh/release.v1 -l name=${RELEASE_NAME} || true
kubectl annotate --overwrite psp gatekeeper-admin meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite psp gatekeeper-admin meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite clusterrole gatekeeper-manager-role meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite clusterrole gatekeeper-manager-role meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite clusterrolebinding gatekeeper-manager-rolebinding meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite clusterrolebinding gatekeeper-manager-rolebinding meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite ValidatingWebhookConfiguration gatekeeper-validating-webhook-configuration meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite ValidatingWebhookConfiguration gatekeeper-validating-webhook-configuration meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite -n gatekeeper-system sa gatekeeper-admin meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite -n gatekeeper-system sa gatekeeper-admin meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite -n gatekeeper-system secret gatekeeper-webhook-server-cert meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite -n gatekeeper-system secret gatekeeper-webhook-server-cert meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite -n gatekeeper-system role gatekeeper-manager-role meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite -n gatekeeper-system role gatekeeper-manager-role meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite -n gatekeeper-system rolebinding gatekeeper-manager-rolebinding meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite -n gatekeeper-system rolebinding gatekeeper-manager-rolebinding meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite -n gatekeeper-system service gatekeeper-webhook-service meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite -n gatekeeper-system service gatekeeper-webhook-service meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite -n gatekeeper-system deployment gatekeeper-audit meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite -n gatekeeper-system deployment gatekeeper-audit meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite -n gatekeeper-system deployment gatekeeper-controller-manager meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite -n gatekeeper-system deployment gatekeeper-controller-manager meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true

kubectl annotate --overwrite -n gatekeeper-system PodDisruptionBudget gatekeeper-controller-manager meta.helm.sh/release-name=${RELEASE_NAME} || true
kubectl annotate --overwrite -n gatekeeper-system PodDisruptionBudget gatekeeper-controller-manager meta.helm.sh/release-namespace=${NEW_NAMESPACE} || true
