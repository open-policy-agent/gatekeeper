package util

import v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func Now() *v1.Time {
	t := v1.Now()
	return &t
}
