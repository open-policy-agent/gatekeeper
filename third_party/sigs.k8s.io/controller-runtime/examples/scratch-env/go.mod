module sigs.k8s.io/controller-runtime/examples/scratch-env

go 1.15

require (
	github.com/spf13/pflag v1.0.5
	sigs.k8s.io/controller-runtime v0.0.0-00010101000000-000000000000
)

replace sigs.k8s.io/controller-runtime => ../..
