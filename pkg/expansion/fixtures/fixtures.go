package fixtures

const (
	TempExpDeploymentExpandsPods = `
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: ExpansionTemplate
metadata:
  name: expand-deployments
spec:
  applyTo:
    - groups: ["apps"]
      kinds: ["Deployment"]
      versions: ["v1"]
  templateSource: "spec.template"
  generatedGVK:
    kind: "Pod"
    group: ""
    version: "v1"
`

	TempExpDeploymentExpandsPodsEnforceDryrun = `
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: ExpansionTemplate
metadata:
  name: expand-deployments
spec:
  applyTo:
    - groups: ["apps"]
      kinds: ["Deployment"]
      versions: ["v1"]
  templateSource: "spec.template"
  generatedGVK:
    kind: "Pod"
    group: ""
    version: "v1"
  enforcementAction: "dryrun"
`

	DeploymentNginx = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  labels:
    app: nginx
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: '80'
        args:
        - "/bin/sh"
`

	DeploymentNoGVK = `
metadata:
  name: nginx-deployment
  labels:
    app: nginx
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: '80'
        args:
        - "/bin/sh"
`
	PodNoMutate = `
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: nginx
  namespace: default
spec:
  containers:
  - args:
    - "/bin/sh"
    image: nginx:1.14.2
    name: nginx
    ports:
    - containerPort: '80'
`

	PodImagePullMutate = `
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: nginx
  namespace: default
spec:
  containers:
  - args:
    - "/bin/sh"
    image: nginx:1.14.2
    imagePullPolicy: Always
    name: nginx
    ports:
    - containerPort: '80'
`

	PodMutateImage = `
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: nginx
  namespace: default
spec:
  containers:
  - args:
    - "/bin/sh"
    image: nginx:v2
    name: nginx
    ports:
    - containerPort: '80'
`

	PodImagePullMutateAnnotated = `
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: nginx
  annotations:
    owner: admin
  namespace: default
spec:
  containers:
  - args:
    - "/bin/sh"
    image: nginx:1.14.2
    imagePullPolicy: Always
    name: nginx
    ports:
    - containerPort: '80'
`

	AssignPullImage = `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: always-pull-image
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  location: "spec.containers[name: *].imagePullPolicy"
  parameters:
    assign:
      value: "Always"
  match:
    source: "Generated"
    scope: Namespaced
    kinds:
      - apiGroups: []
        kinds: []
`

	AssignPullImageSourceAll = `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: always-pull-image
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  location: "spec.containers[name: *].imagePullPolicy"
  parameters:
    assign:
      value: "Always"
  match:
    source: "Generated"
    scope: Namespaced
    kinds:
      - apiGroups: []
        kinds: []
`

	AssignPullImageSourceEmpty = `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: always-pull-image
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  location: "spec.containers[name: *].imagePullPolicy"
  parameters:
    assign:
      value: "Always"
  match:
    scope: Namespaced
    kinds:
      - apiGroups: []
        kinds: []
`

	AssignImage = `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: AssignImage
metadata:
  name: tag-v2
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  location: "spec.containers[name:nginx].image"
  parameters:
    assignTag: ":v2"
`

	AssignHostnameSourceOriginal = `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assign-hostname
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  location: "spec.containers[name: *].hostname"
  parameters:
    assign:
      value: "ThisShouldNotBeSet"
  match:
    source: "Original"
    scope: Namespaced
    kinds:
      - apiGroups: []
        kinds: []
`

	AssignMetaAnnotatePod = `
apiVersion: mutations.gatekeeper.sh/v1beta1
kind: AssignMetadata
metadata:
  name: demo-annotation-owner
spec:
  match:
    source: Generated
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Pod"]
  location: "metadata.annotations.owner"
  parameters:
    assign:
      value:  "admin"
`
	AssignMetaAnnotateKitten = `
apiVersion: mutations.gatekeeper.sh/v1beta1
kind: AssignMetadata
metadata:
  name: demo-annotation-meow
spec:
  match:
    source: Generated
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Kitten"]
  location: "metadata.annotations.sound"
  parameters:
    assign:
      value:  "meow"
`

	TemplateCatExpandsKitten = `
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: ExpansionTemplate
metadata:
  name: expand-cats-kitten
spec:
  applyTo:
    - groups: ["cat.myapp.sh"]
      kinds: ["Cat"]
      versions: ["v1alpha1"]
  templateSource: "spec.catStuff"
  generatedGVK:
    kind: "Kitten"
    group: "kitten.myapp.sh"
    version: "v1alpha1"
  enforcementAction: "dryrun"
`

	TemplateCatExpandsPurr = `
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: ExpansionTemplate
metadata:
  name: expand-cats-purr
spec:
  applyTo:
    - groups: ["cat.myapp.sh"]
      kinds: ["Cat"]
      versions: ["v1alpha1"]
  templateSource: "spec.purrStuff"
  generatedGVK:
    kind: "Purr"
    group: "purr.myapp.sh"
    version: "v1alpha1"
  enforcementAction: "warn"
`

	GeneratorCat = `
apiVersion: cat.myapp.sh/v1alpha1
kind: Cat
metadata:
  name: big-chungus
spec:
  catStuff:
    metadata:
      labels:
        fluffy: extremely
    spec:
      breed: calico
      weight: 10
  purrStuff:
    spec:
      loud: very
`

	AssignKittenAge = `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assign-kitten-age
spec:
  applyTo:
  - groups: ["kitten.myapp.sh"]
    kinds: ["Kitten"]
    versions: ["v1alpha1"]
  location: "spec.age"
  parameters:
    assign:
      value: 10
  match:
    source: "Generated"
    scope: Namespaced
    kinds:
      - apiGroups: []
        kinds: []
`

	AssignMetaAnnotatePurr = `
apiVersion: mutations.gatekeeper.sh/v1beta1
kind: AssignMetadata
metadata:
  name: annotate-purr
spec:
  match:
    source: Generated
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Purr"]
  location: "metadata.annotations.shouldPet"
  parameters:
    assign:
      value:  "manytimes"
`

	ResultantKitten = `
apiVersion: kitten.myapp.sh/v1alpha1
kind: Kitten
metadata:
  annotations:
    sound: meow
  labels:
    fluffy: extremely
  namespace: default
spec:
  breed: calico
  weight: 10
  age: 10
`

	ResultantPurr = `
apiVersion: purr.myapp.sh/v1alpha1
kind: Purr
metadata:
  annotations:
    shouldPet: manytimes
  namespace: default
spec:
  loud: very
`
)
