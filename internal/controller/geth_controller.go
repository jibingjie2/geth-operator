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
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"regexp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
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
	logger := logf.FromContext(ctx)
	var geth xttv1.Geth
	if err := r.Get(ctx, req.NamespacedName, &geth); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "Pending", "Pending")

	cmName := getConfigMapName(geth.Name)
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: geth.Namespace}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("配置 ConfigMap 不存在，准备创建", "configmap", cmName)

			newCM := buildConfigMap(&geth)

			if err := ctrl.SetControllerReference(&geth, newCM, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, newCM); err != nil {
				_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "ConfigCreated", "The configuration file has been created")
				return ctrl.Result{}, err
			}
			logger.Info("ConfigMap 创建完成", "configmap", cmName)

			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		} else {
			return ctrl.Result{}, err
		}
	}

	jobName := getInitJobName(geth.Name)
	job := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: geth.Namespace}, job); err != nil {
		if apierrors.IsNotFound(err) {
			//	Job不存在,创建
			logger.Info("初始化 Job 不存在，准备创建", "job", jobName)
			initJob := buildInitJob(&geth)
			// 设置 ownerReference，让 Job 跟随 Geth 生命周期
			if err := ctrl.SetControllerReference(&geth, initJob, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			// 创建Job
			if err := r.Create(ctx, initJob); err != nil {
				_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "InitJobRunning", "Initialize Job Running")
				return ctrl.Result{}, err
			}
			logger.Info("初始化 Job 已创建", "job", jobName)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		} else {
			//	其它错误,返回err
			return ctrl.Result{}, err
		}
	} else {
		//	没有错误,判断job状态
		if job.Status.Succeeded < 1 {
			//	job未完成,requeue
			_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "InitJobRunning", "Initialize Job Running")
			return ctrl.Result{RequeueAfter: time.Second * 5}, nil
		} else {
			//job完成,写入地址到gethStatus
			podList := &corev1.PodList{}
			if err := r.List(ctx, podList, client.InNamespace(geth.Namespace), client.MatchingLabels{
				"job-name": jobName,
			}); err != nil {
				logger.Error(err, "列出 Job 对应的 Pod 失败")
				return ctrl.Result{}, err
			}

			if len(podList.Items) == 0 {
				logger.Info("找不到 Job 对应的 Pod，等待中…")
				return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
			}
			pod := &podList.Items[0]
			logs, err := getPodLogs(ctrl.GetConfigOrDie(), pod, pod.Spec.Containers[0].Name)
			if err != nil {
				logger.Error(err, "读取 Pod 日志失败")
				return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
			}
			var addr string
			for _, line := range strings.Split(logs, "\n") {
				// 使用正则表达式匹配以 0x 开头的 40 位十六进制字符串
				re := regexp.MustCompile(`0x[a-fA-F0-9]{40}`)
				match := re.FindString(line)
				if match != "" {
					addr = match
					break
				}
			}
			if addr == "" {
				logger.Info("未从日志中解析出地址，等待重试")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			logger.Info("从日志中解析出地址", "address", addr)
			geth.Status.AccountAddress = addr
			if err := r.Update(ctx, &geth); err != nil {
				logger.Error(err, "更新 Geth Status 失败")
				return ctrl.Result{}, err
			}

			// 修改配置文件的address

			original := cm.Data["genesis.json"]
			patched, err := patchGenesisExtraData(original, geth.Status.AccountAddress)
			if err != nil {
				logger.Error(err, "修改 extraData 失败")
				return ctrl.Result{}, err
			}

			cm.Data["genesis.json"] = patched
			if err := r.Update(ctx, cm); err != nil {
				return ctrl.Result{}, err
			}

			depName := geth.Name
			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, types.NamespacedName{
				Namespace: geth.Namespace,
				Name:      geth.Name,
			}, dep); err != nil {
				if apierrors.IsNotFound(err) {
					logger.Info("准备创建 Geth 节点 Deployment")
					dep := buildDeployment(&geth)
					if err := ctrl.SetControllerReference(&geth, dep, r.Scheme); err != nil {
						return ctrl.Result{}, err
					}
					if err := r.Create(ctx, dep); err != nil {
						return ctrl.Result{}, err
					}

					_ = updateStatusPhase(ctx, r.Status(), r.Recorder, &geth, "Running", "Ready")

					logger.Info("Deployment 已创建", "deployment", depName)
					svc := &corev1.Service{}
					svcName := geth.Name
					if err := r.Get(ctx, types.NamespacedName{
						Namespace: geth.Namespace,
						Name:      geth.Name,
					}, svc); err != nil {
						if apierrors.IsNotFound(err) {
							logger.Info("准备创建 Geth Service", "svc", svcName)
							svc := buildService(&geth)
							if err := ctrl.SetControllerReference(&geth, svc, r.Scheme); err != nil {
								return ctrl.Result{}, err
							}

							if err := r.Create(ctx, svc); err != nil {
								return ctrl.Result{}, err
							}
							logger.Info("Service 创建完成", "svc", svcName)
							return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
						} else {
							return ctrl.Result{}, err
						}
					}
					return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
				} else {
					return ctrl.Result{}, err
				}
			}
		}
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
