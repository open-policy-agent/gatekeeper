This is a demo of a prototype-stage feature and is subject to change.

The demo will not work unless the `--experimental-enable-k8s-native-validation`` is
set. Please set `--validate-template-rego` to `false` if using gatekeeper version 3.13.1+ but before 3.16.0.

Note that the contents of the constraint template have changed since cutting
Gatekeeper's v3.13.0 release. To try this with the development build of
Gatekeeper, use a [dev image](https://open-policy-agent.github.io/gatekeeper/website/docs/install/#deploying-a-release-using-development-image).

<img width= "900" height="500" src="demo.gif" alt="cel demo">