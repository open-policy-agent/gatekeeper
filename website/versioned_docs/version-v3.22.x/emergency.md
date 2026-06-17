---
id: emergency
title: Emergency Recovery
---

If a situation arises where Gatekeeper is preventing the cluster from operating correctly,
the webhook can be disabled. This will remove all Gatekeeper admission checks. Assuming
the default webhook name has been used this can be achieved by running:

`kubectl delete validatingwebhookconfigurations.admissionregistration.k8s.io gatekeeper-validating-webhook-configuration`

Redeploying the webhook configuration will re-enable Gatekeeper.