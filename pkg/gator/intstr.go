package gator

import "k8s.io/apimachinery/pkg/util/intstr"

func intStrFromInt(val int) *intstr.IntOrString {
	result := intstr.FromInt(val)
	return &result
}

func intStrFromStr(val string) *intstr.IntOrString {
	result := intstr.FromString(val)
	return &result
}
