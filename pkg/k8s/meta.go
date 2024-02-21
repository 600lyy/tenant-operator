package k8s

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetAnnotation(obj metav1.Object, annotation string) (string, bool) {
	if annotations := obj.GetAnnotations(); annotations != nil {
		val, ok := annotations[annotation]
		return val, ok
	}
	return "", false
}
