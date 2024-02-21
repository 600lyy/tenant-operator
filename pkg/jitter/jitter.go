package jitter

import (
	"fmt"
	"strconv"
	"time"

	"github.com/600lyy/tenant-operator/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	MeanReconcileReenqueueInverval = 10 * time.Minute
)

func GenerateJitterReenqueuePeriod(obj metav1.Object) (time.Duration, error) {
	if val, ok := k8s.GetAnnotation(obj, k8s.ReconcileIntervalInSecondsAnnotation); ok {
		if reconcileInseconds, err := MeanReconcileReenqueuePeriodFromAnnotation(val); err != nil {
			return 0, err
		} else {
			return wait.Jitter(reconcileInseconds/2, k8s.JitterFactor), nil
		}
	}
	return wait.Jitter(MeanReconcileReenqueueInverval/2, k8s.JitterFactor), nil
}

func MeanReconcileReenqueuePeriodFromAnnotation(val string) (time.Duration, error) {
	intervalInSeconds, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("error converting the string %s's value %s to int32", k8s.ReconcileIntervalInSecondsAnnotation, val)
	}
	if intervalInSeconds < 0 {
		return 0, fmt.Errorf("reconcileInvervalInAnnotation can't be negative, %s is set in the annotation", val)
	}
	return time.Duration(intervalInSeconds) * time.Second, nil
}
