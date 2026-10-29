/*
Copyright 2026.

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
	appsv1alpha1 "github.com/devgalsen/goapp-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	//apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// GoAppReconciler reconciles a GoApp object
type GoAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=apps.senit.dev,resources=goapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.senit.dev,resources=goapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.senit.dev,resources=goapps/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GoApp object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/reconcile
func (r *GoAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	//lire les ressources  GoApp
	var app appsv1alpha1.GoApp
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	labels := map[string]string{
		"app.kubernetes.io/name": app.Name,
	}
	//construire le deploiement souhaite
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
	}
	// TODO(user): your logic here
	_, err := controllerutil.CreateOrUpdate(
		ctx, r.Client, deployment,
		func() error {
			if err := ctrl.SetControllerReference(
				&app, deployment, r.Scheme,
			); err != nil {
				return err
			}

			replicas := app.Spec.Replicas

			deployment.Labels = labels
			deployment.Spec = appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: app.Spec.Image,
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										ContainerPort: app.Spec.Port,
									},
								},
							},
						},
					},
				},
			}

			return nil
		},
	)
	if err != nil {
		logf.FromContext(ctx).Error(err, "unable to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// 3. Construire le Service souhaité.
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(
		ctx, r.Client, service,
		func() error {
			if err := ctrl.SetControllerReference(
				&app, service, r.Scheme,
			); err != nil {
				return err
			}

			service.Labels = labels
			service.Spec = corev1.ServiceSpec{
				Type:     corev1.ServiceTypeClusterIP,
				Selector: labels,
				Ports: []corev1.ServicePort{
					{
						Name:       "http",
						Port:       app.Spec.Port,
						TargetPort: intstr.FromString("http"),
					},
				},
			}

			return nil
		},
	)

	if err != nil {
		logf.FromContext(ctx).Error(err, "unable to reconcile Service")
		return ctrl.Result{}, err
	}

	log.Info("GoApp reconciled",
		"name", app.Name,
		"namespace", app.Namespace,
	)
	if err := r.Get(ctx, client.ObjectKey{
		Name:      app.Name,
		Namespace: app.Namespace,
	}, deployment); err != nil {
		return ctrl.Result{}, err
	}
	// Keep the current status as the patch base.
	base := app.DeepCopy()

	// Synchronize observed replica counts.
	app.Status.Replicas = deployment.Status.Replicas
	app.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	// Determine whether the application is ready.
	ready := deployment.Spec.Replicas != nil && *deployment.Spec.Replicas > 0 && deployment.Status.ReadyReplicas >= *deployment.Spec.Replicas
	conditionStatus := metav1.ConditionFalse
	reason := "Progressing"
	message := "Waiting for all replicas to become ready"

	if ready {
		conditionStatus = metav1.ConditionTrue
		reason = "Available"
		message = "All desired replicas are ready"
	}
	meta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             conditionStatus,
		ObservedGeneration: app.Generation,
		Reason:             reason,
		Message:            message,
	})
	if err := r.Status().Patch(ctx, &app, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GoAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.GoApp{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("goapp").
		Complete(r)
}
