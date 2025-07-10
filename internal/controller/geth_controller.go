/*
Copyright 2025.

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
	xttv1 "github.com/jibingjie/geth-operator/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

// GethReconciler reconciles a Geth object
type GethReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder // 事件记录
}

// +kubebuilder:rbac:groups=xtt.xyz,resources=geths,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=xtt.xyz,resources=geths/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=xtt.xyz,resources=geths/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Geth object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *GethReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var geth xttv1.Geth
	if err := r.Get(ctx, req.NamespacedName, &geth); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "Pending", "Pending")

	//确保 ConfigMap 存在，如果不存在则创建
	cmCreated, err := r.ensureCM(ctx, &geth)
	if err != nil {
		return ctrl.Result{}, err
	}

	//如果是新创建的ConfigMap,则更新Geth状态,并requeue
	if cmCreated {
		_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "ConfigCreated", "The configuration file has been created")
		return ctrl.Result{}, nil
	}

	jobCreated, err := r.ensureInitJob(ctx, &geth)
	if err != nil {
		return ctrl.Result{}, err
	}

	if jobCreated {
		_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "InitJobRunning", "Initialize Job Running")
		return ctrl.Result{}, nil
	}

	//更新实例的状态,包括地址
	updated, err := r.patchGethStatus(ctx, &geth)
	//初始化job未完成,无法更新状态,等待2s
	if IsPodNotCompletionError(err) {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, err
	}

	if updated {
		cmUpdated, err := r.patchCMAddress(ctx, &geth)
		if err != nil {
			return ctrl.Result{}, err
		}

		if cmUpdated {
			return ctrl.Result{}, nil
		}
	}

	dpCreated, err := r.ensureDeployment(ctx, &geth)
	if err != nil {
		return ctrl.Result{}, err
	}
	if dpCreated {
		//_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "Running", "Ready")
		return ctrl.Result{}, nil
	}

	svcCreated, err := r.ensureSvc(ctx, &geth)
	if err != nil {
		return ctrl.Result{}, err
	}
	if svcCreated {
		_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "Running", "Ready")
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GethReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&xttv1.Geth{}).
		Named("geth").
		Complete(&GethReconciler{
			Client:   mgr.GetClient(),
			Scheme:   mgr.GetScheme(),
			Recorder: mgr.GetEventRecorderFor("geth-conrtoller"),
		})
}
