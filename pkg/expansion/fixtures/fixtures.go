package fixtures

const (
	TempExpDeploymentExpandsPods = `
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: TemplateExpansion
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

	PodImagePullMutateAnnotated = `
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: nginx
  annotations:
    owner: admin
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
  name: demo-annotation-owner
spec:
  match:
    origin: Generated
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Cat"]
  location: "metadata.annotations.owner"
  parameters:
    assign:
      value:  "meow"
`

	TemplateCatKitten = `
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: TemplateExpansion
metadata:
  name: expand-cats-kitten
spec:
  applyTo:
    - groups: ["cat.myapp.sh"]
      kinds: ["Cat"]
      versions: ["v1alpha1"]
  templateSource: "spec.cat-stuff"
  generatedGVK:
    kind: "Kitten"
    group: "kitten.myapp.sh"
    version: "v1alpha1"
`

	TemplateCatPurr = `
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: TemplateExpansion
metadata:
  name: expand-cats-purr
spec:
  applyTo:
    - groups: ["cat.myapp.sh"]
      kinds: ["Cat"]
      versions: ["v1alpha1"]
  templateSource: "spec.purr-stuff"
  generatedGVK:
    kind: "Purr"
    group: "purr.myapp.sh"
    version: "v1alpha1"
`

	GeneratorCat = `
apiVersion: cat.myapp.sh/v1alpha1
kind: Cat
metadata:
  name: big-chungus
spec:
  cat-stuff:
    metadata:
      labels:
        fluffy: extremely
    spec:
      breed: calico
      weight: 10
  purr-stuff:
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
  location: "metadata.annotations.owner"
  parameters:
    assign:
      value:  "admin"
`

	ResultantKitten = `
apiVersion: kitten.myapp.sh/v1alpha1
kind: Kitten
metadata:
  labels:
    fluffy: extremely
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
    owner: admin
spec:
  loud: very
`
)
