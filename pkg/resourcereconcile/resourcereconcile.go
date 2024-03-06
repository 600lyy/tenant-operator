package resourcereconcile

import (
	"k8s.io/apimachinery/pkg/runtime"
)

func ShouldSkip(obj runtime.Object) bool {

	return false
}
