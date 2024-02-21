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

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multitenancyv1 "github.com/600lyy/tenant-operator/api/v1"
	"github.com/600lyy/tenant-operator/pkg/jitter"
)

var (
	tenantOperatorAnnotation = "tenant.600lyy.io/tenant-operator"
	finalizerName            = "tenant.600lyy.io/finalizer"
)

// TenantReconciler reconciles a Tenant object
type TenantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=multitenancy.600lyy.io,resources=tenants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multitenancy.600lyy.io,resources=tenants/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=multitenancy.600lyy.io,resources=tenants/finalizers,verbs=update
//+kubebuilder:rbac:groups=multitenancy.600lyy.io,resources=*,verbs=*
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=*,verbs=*

func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// TODO(user): your logic here
	tenant := multitenancyv1.Tenant{}

	log.Info("Reconciling tenant")

	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		log.Error(err, "Object not found")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// When user attempts to delete a resource, the API server handling the delete
	// request notices the values in the "finalizer" field and add a metadata.deletionTimestamp
	// field with time user started the deleteion. This field indicates the deletion of the object
	// has been requested, but the deletion will not be complete until all the finailzers are removed.
	// For more details, check Finalizer and Deletion:
	// - https://kubernetes.io/blog/2021/05/14/using-finalizers-to-control-deletion/
	if tenant.ObjectMeta.DeletionTimestamp.IsZero() {
		// Add a finalizer if not present
		if !controllerutil.ContainsFinalizer(&tenant, finalizerName) {
			log.Info("Adding finalizer to the tenant", "Tenant", tenant.Name)
			tenant.ObjectMeta.Finalizers = append(tenant.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(ctx, &tenant); err != nil {
				log.Error(err, "unable to update Tenant", "tenant", tenant.Name)
				return ctrl.Result{}, err
			}
		}

		// Loop through each namespace defined in the Tenant Spec
		// Ensure the namespace exists, and if not, create it
		// Then ensure RoleBindings for each namespace
		for _, ns := range tenant.Spec.Namespaces {
			log.Info("Ensuring namespace", "namespace", ns)
			if err := r.ensureNamespace(ctx, &tenant, ns); err != nil {
				log.Error(err, "unable to ensure namespace", "namespace", ns)
				return ctrl.Result{}, err
			}
		}

		// List namespaces that are owned by this tenant but not found in the spec
		// Add all namespaces to the observed status
		var namespaceList corev1.NamespaceList
		if err := r.List(ctx, &namespaceList); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to list namespaces: %v", err)
		}

		// observedNamespaces := tenant.Status.ObservedNamespaces

		for _, ns := range namespaceList.Items {
			if admin, ok := ns.Annotations["adminEmail"]; ok && admin == tenant.Spec.AdminEmail {
				if !namespaceExistInList(ns.Name, tenant.Status.ObservedNamespaces) {
					tenant.Status.ObservedNamespaces = append(tenant.Status.ObservedNamespaces, ns.Name)
				}
			}
		}

		tenant.Status.NamespaceCount = len(tenant.Status.ObservedNamespaces)
		tenant.Status.AdminEmail = tenant.Spec.AdminEmail
		if err := r.Status().Update(ctx, &tenant); err != nil {
			log.Error(err, "unable to update Tenant status")
			return ctrl.Result{}, err
		}

		if reconcileReenqueuePeriod, err := jitter.GenerateJitterReenqueuePeriod(&tenant); err != nil {
			// log.Error(err, "unable to generate the next resync period", "tenant", tenant.Name)
			return ctrl.Result{}, err
		} else {
			log.Info("successully finished reconcile", "tenant", tenant.Name, "time to next reconcile", reconcileReenqueuePeriod)
			return ctrl.Result{RequeueAfter: reconcileReenqueuePeriod}, nil
		}

		// the object is marked for deletion pening the fianliers
	} else {
		if controllerutil.ContainsFinalizer(&tenant, finalizerName) {
			log.Info("Finalizer found, cleaning up the resource")
			if err := r.cleanupExternalResources(ctx, &tenant); err != nil {
				log.Error(err, "Failed to cleanup resources", "tenant", tenant.Name)
				return ctrl.Result{}, err
			}
			log.Info("Resource cleanup succeeded", "tenant", tenant.Name)

			// Remove the finalizer from the Tenant object once the cleanup succeded
			controllerutil.RemoveFinalizer(&tenant, finalizerName)
			if err := r.Update(ctx, &tenant); err != nil {
				log.Error(err, "Unable to remove finalizer and update Tenant", "Tenant", tenant.Name)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *TenantReconciler) ensureNamespace(ctx context.Context, tenant *multitenancyv1.Tenant, namespaceName string) error {
	log := log.FromContext(ctx)

	namespace := corev1.Namespace{}

	//Attempt to fetch the namespace, otherwise creat a new one if not exist
	err := r.Get(ctx, client.ObjectKey{Name: namespaceName}, &namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating namespace", "ns", namespaceName)
			namespace = corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
					Annotations: map[string]string{
						"adminEmail": tenant.Spec.AdminEmail,
						"managed-by": tenantOperatorAnnotation,
					},
				},
			}
			if err = r.Create(ctx, &namespace); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		log.Info("Namespace already exists", "namespace", namespaceName)

		if namespace.Annotations == nil {
			namespace.Annotations = map[string]string{}
		}

		requiredAnnotions := map[string]string{
			"adminEmail": tenant.Spec.AdminEmail,
			"managed-by": tenantOperatorAnnotation,
		}

		for annotaionKey, requiredValue := range requiredAnnotions {
			existingAnnotation, ok := namespace.Annotations[annotaionKey]
			if !ok || existingAnnotation != requiredValue {
				log.Info("Updating namespace annotation", "namespace", namespace.Name, "annotation", annotaionKey)
				namespace.Annotations[annotaionKey] = requiredValue
				if err := r.Update(ctx, &namespace); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *TenantReconciler) cleanupExternalResources(ctx context.Context, tenant *multitenancyv1.Tenant) error {
	log := log.FromContext(ctx)

	for _, ns := range tenant.Status.ObservedNamespaces {
		namespace := corev1.Namespace{}
		namespace.Name = ns

		if err := r.Delete(ctx, &namespace); client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to delete the namespace", "namespace", ns)
			return err
		}
		log.Info("Namespace deleted", "namespace", ns)
	}
	log.Info("All resources deleted for tenant", "tenant", tenant.Name)
	return nil
}

func namespaceExistInList(namespace string, namespacesList []string) bool {
	for _, ns := range namespacesList {
		if ns == namespace {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&multitenancyv1.Tenant{}).
		Complete(r)
}
