package gator

import "k8s.io/apimachinery/pkg/util/intstr"

func IntStrFromInt(val int) *intstr.IntOrString {
	result := intstr.FromInt(val)
	return &result
}

func IntStrFromStr(val string) *intstr.IntOrString {
	result := intstr.FromString(val)
	return &result
}
