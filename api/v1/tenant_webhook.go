/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var tenantlog = logf.Log.WithName("tenant-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *Tenant) SetupWebhookWithManager(mgr ctrl.Manager) error {
	validator := &TenantValidator{
		Client: mgr.GetClient(),
	}

	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(validator).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-multitenancy-600lyy-io-v1-tenant,mutating=false,failurePolicy=fail,sideEffects=None,groups=multitenancy.600lyy.io,resources=tenants,verbs=create;update,versions=v1,name=vtenant.kb.io,admissionReviewVersions=v1

//We need to validate if the namespaces specified in a tenant already exist
//This cannot be validated by the tenant object itself, therefore we need a custom validator
//var _ webhook.Validator = &Tenant{}

// Disable deepcopy generation for this type, see https://book.kubebuilder.io/reference/markers/object.
//+kubebuilder:object:generate=false

type TenantValidator struct {
	client.Client
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *TenantValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	tenant, ok := obj.(*Tenant)
	if !ok {
		return nil, fmt.Errorf("unexpected object type, expected Tenant type")
	}

	tenantlog.Info("validate create for", "name", tenant.Name)

	var namespaces corev1.NamespaceList

	if err := r.List(ctx, &namespaces); err != nil {
		return nil, fmt.Errorf("Failed to list namespaces: %v", err)
	}

	for _, ns := range tenant.Spec.Namespaces {
		if namespaceExists(namespaces, ns) {
			return nil, fmt.Errorf("namespace %s already exists and is managed by another tenant", ns)
		}
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *TenantValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldTenant, ok := oldObj.(*Tenant)
	if !ok {
		return nil, fmt.Errorf("unexpected object type, expected Tenant type")
	}

	newTenant, ok := newObj.(*Tenant)
	if !ok {
		return nil, fmt.Errorf("unexpected object type, expected Tenant type")
	}

	tenantlog.Info("validate update for", "name", oldTenant.Name)

	var namespaces corev1.NamespaceList

	if err := r.List(ctx, &namespaces); err != nil {
		return nil, fmt.Errorf("Failed to list namespaces: %v", err)
	}

	for _, ns := range newTenant.Spec.Namespaces {
		if !func(namespaceList []string, namespace string) bool {
			for _, ns := range namespaceList {
				if ns == namespace {
					return true
				}
			}
			return false
		}(oldTenant.Spec.Namespaces, ns) {
			if namespaceExists(namespaces, ns) {
				return nil, fmt.Errorf("namespace %s already exists and is managed by another tenant", ns)
			}
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *TenantValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {

	// tenantlog.Info("validate delete for", "name", "Placeholder")

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func namespaceExists(namespaces corev1.NamespaceList, namespace string) bool {
	for _, n := range namespaces.Items {
		if n.Name == namespace {
			return true
		}
	}
	return false
}
