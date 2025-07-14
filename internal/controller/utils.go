package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	xttv1 "github.com/jibingjie/geth-operator/api/v1"
	"github.com/jibingjie/geth-operator/internal/templates"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/pointer"
	"regexp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

func execInPod(config *rest.Config, pod *corev1.Pod, command []string) (string, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", err
	}

	req := clientset.CoreV1().RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		Param("container", pod.Spec.Containers[0].Name).
		Param("stdout", "true").
		Param("stderr", "true").
		Param("stdin", "false").
		Param("tty", "false")

	for _, c := range command {
		req.Param("command", c)
	}

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", err
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})
	if err != nil {
		return "", fmt.Errorf("执行失败: %v，stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

func getInitJobName(gethName string) string {
	return gethName + "-init"
}

func getPVCName(geth *xttv1.Geth) string {
	pvcName := geth.Spec.PVCName
	if pvcName == "" {
		// 返回错误或者用默认PVC名
		//return ctrl.Result{}, fmt.Errorf("spec.pvcName must be set")
	}
	return pvcName
}

func getConfigMapName(gethName string) string {
	return gethName + "-config"
}

func getPodLogs(config *rest.Config, pod *corev1.Pod, containerName string) (string, error) {
	// 初始化 Kubernetes 客户端
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("创建 clientset 失败: %w", err)
	}

	req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: containerName,
	})

	podLogs, err := req.Stream(context.Background())
	if err != nil {
		return "", fmt.Errorf("获取日志流失败: %w", err)
	}
	defer podLogs.Close()

	var result string
	scanner := bufio.NewScanner(podLogs)
	for scanner.Scan() {
		line := scanner.Text()
		result += line + "\n"
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("读取日志时发生错误: %w", err)
	}

	return result, nil
}

func buildInitJob(geth *xttv1.Geth) *batchv1.Job {
	jobName := getInitJobName(geth.Name)

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: geth.Namespace,
			Labels: map[string]string{
				"app":     "geth",
				"geth_cr": geth.Name,
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "init",
							Image:   geth.Spec.Image,
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								`geth init --datadir /data /data/genesis.json && \
ADDR=$(geth account new --datadir /data --password /data/password.txt | grep 'Public address' | awk '{print $NF}') && \
echo $ADDR > /data/account.txt && \
cat /data/account.txt`,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
								{
									Name:      "config",
									MountPath: "/data/genesis.json",
									SubPath:   "genesis.json",
								},
								{
									Name:      "config",
									MountPath: "/data/password.txt",
									SubPath:   "password.txt",
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: getPVCName(geth),
								},
							},
						},
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: getConfigMapName(geth.Name),
									},
								},
							},
						},
					},
				},
			},
		},
		Status: batchv1.JobStatus{},
	}

}

func buildConfigMap(geth *xttv1.Geth) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getConfigMapName(geth.Name),
			Namespace: geth.Namespace,
			Labels: map[string]string{
				"app":     "geth",
				"geth_cr": geth.Name,
			},
		},
		Immutable: nil,
		Data: map[string]string{
			"password.txt": geth.Spec.Password,
			"genesis.json": templates.DefaultGenesisJSON,
		},
		BinaryData: nil,
	}
}

func buildDeployment(geth *xttv1.Geth) *appsv1.Deployment {

	labels := map[string]string{
		"app":     "geth",
		"geth_cr": geth.Name,
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      geth.Name,
			Namespace: geth.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32(1),
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
							Name:    "geth",
							Image:   geth.Spec.Image,
							Command: geth.Spec.Cmd,
							Args:    geth.Spec.Args,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8545,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
								{
									Name:      "config",
									MountPath: "/data/genesis.json",
									SubPath:   "genesis.json",
								},
								{
									Name:      "config",
									MountPath: "/data/password.txt",
									SubPath:   "password.txt",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: getPVCName(geth),
								},
							},
						},
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: getConfigMapName(geth.Name),
									},
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{},
	}
}

func buildService(geth *xttv1.Geth) *corev1.Service {
	labels := map[string]string{
		"app":     "geth",
		"geth_cr": geth.Name,
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      geth.Name,
			Namespace: geth.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "rpc",
					Port:       8545,
					TargetPort: intstr.FromInt(8545),
				},
			},
			Type: corev1.ServiceTypeClusterIP, // 可改为 NodePort / LoadBalancer
		},
	}
}

// 更新状态并记录事件
func updateStatusPhase(ctx context.Context, writer client.SubResourceWriter, recorder record.EventRecorder, geth *xttv1.Geth, phase string, message string) error {
	if geth.Status.Phase != phase {
		geth.Status.Phase = phase
		recorder.Event(geth, corev1.EventTypeNormal, phase, message)
		return writer.Update(ctx, geth)
	}
	return nil
}

func patchGenesisExtraData(genesisJSON string, accountAddr string) (string, error) {
	if !strings.HasPrefix(accountAddr, "0x") || len(accountAddr) != 42 {
		return "", fmt.Errorf("非法地址格式: %s", accountAddr)
	}

	addrHex := accountAddr[2:] // 去掉 "0x"前缀

	var genesis map[string]interface{}
	if err := json.Unmarshal([]byte(genesisJSON), &genesis); err != nil {
		return "", fmt.Errorf("解析 genesis JSON 失败: %w", err)
	}

	extraData, ok := genesis["extraData"].(string)
	if !ok || !strings.HasPrefix(extraData, "0x") || len(extraData) < 106 {
		return "", fmt.Errorf("extraData 字段非法或过短")
	}

	// 替换中间 signer 地址部分（第 33 字节起，即 offset=66，替换 40 个 hex 字符）
	newExtra := extraData[:66] + addrHex + extraData[106:]
	genesis["extraData"] = newExtra

	modified, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return "", fmt.Errorf("修改 genesis 后序列化失败: %w", err)
	}
	return string(modified), nil
}

// 确保 Geth 资源所需的 ConfigMap 存在，如果不存在则创建
func (r *GethReconciler) ensureCM(ctx context.Context, geth *xttv1.Geth) (bool, error) {
	logger := logf.FromContext(ctx)
	//获取cm名称
	cmName := getConfigMapName(geth.Name)
	//cm对象
	cm := &corev1.ConfigMap{}
	//查询指定的命名空间是否存在cm
	if err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: geth.Namespace}, cm); err != nil {
		//cm不存在, 创建
		if apierrors.IsNotFound(err) {
			logger.Info("配置 ConfigMap 不存在，准备创建", "configmap", cmName)
			//构造新的cm对象
			newCM := buildConfigMap(geth)
			//设置 ownerReference，确保 ConfigMap 会随 Geth 被删除
			if err := ctrl.SetControllerReference(geth, newCM, r.Scheme); err != nil {
				return false, err
			}
			//创建 ConfigMap 资源
			if err := r.Create(ctx, newCM); err != nil {
				return false, err
			}
			logger.Info("ConfigMap 创建完成", "configmap", cmName)
			//ConfigMap已创建
			return true, nil
		} else {
			//其它错误
			return false, err
		}
	}
	//ConfigMap已存在,无需处理
	return false, nil
}

func (r *GethReconciler) ensureInitJob(ctx context.Context, geth *xttv1.Geth) (bool, error) {
	logger := logf.FromContext(ctx)
	jobName := getInitJobName(geth.Name)
	job := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: geth.Namespace}, job); err != nil {
		if apierrors.IsNotFound(err) {
			//	Job不存在,创建
			logger.Info("初始化 Job 不存在，准备创建", "job", jobName)
			initJob := buildInitJob(geth)
			// 设置 ownerReference，Job 跟随 Geth 生命周期
			if err := ctrl.SetControllerReference(geth, initJob, r.Scheme); err != nil {
				return false, err
			}
			// 创建Job
			if err := r.Create(ctx, initJob); err != nil {
				return false, err
			}
			logger.Info("初始化 Job 已创建", "job", jobName)
			return true, nil
		} else {
			//	其它错误,返回err
			return false, err
		}
	} else {
		return false, err
	}
}

func (r *GethReconciler) waitInitJobCompletion(ctx context.Context, geth *xttv1.Geth) (bool, error) {
	jobName := getInitJobName(geth.Name)
	job := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: geth.Namespace}, job); err != nil {
		return false, err
	}

	if job.Status.Succeeded < 1 {
		_ = updateStatusPhase(ctx, r.Status(), r.Recorder, geth, "InitJobRunning", "Initialize Job Running")
		//return false, PodNotCompletionError("Init job not complete")
		return false, nil
	} else {
		return true, nil
	}
}

func (r *GethReconciler) patchGethStatus(ctx context.Context, geth *xttv1.Geth) (bool, error) {
	logger := logf.FromContext(ctx)
	jobName := getInitJobName(geth.Name)
	isOk, err := r.waitInitJobCompletion(ctx, geth)
	if err != nil && !isOk {
		//	job未完成,requeue
		return false, err
	}
	if isOk {
		//job完成,写入地址到gethStatus
		podList := &corev1.PodList{}
		if err := r.List(ctx, podList, client.InNamespace(geth.Namespace), client.MatchingLabels{
			"job-name": jobName,
		}); err != nil {
			logger.Error(err, "列出 Job 对应的 Pod 失败")
			return false, err
		}

		if len(podList.Items) == 0 {
			logger.Info("找不到 Job 对应的 Pod，等待中…")
			return false, err
		}
		pod := &podList.Items[0]
		logs, err := getPodLogs(ctrl.GetConfigOrDie(), pod, pod.Spec.Containers[0].Name)
		if err != nil {
			logger.Error(err, "读取 Pod 日志失败")
			return false, err
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
			return false, err
		}
		logger.Info("从日志中解析出地址", "address", addr)
		geth.Status.AccountAddress = addr
		if err := r.Status().Update(ctx, geth); err != nil {
			logger.Error(err, "更新 Geth Status 失败")
			return false, err
		}
		return true, nil
	}
	return false, err
}

func (r *GethReconciler) patchCMAddress(ctx context.Context, geth *xttv1.Geth) (bool, error) {
	logger := logf.FromContext(ctx)
	cmName := getConfigMapName(geth.Name)
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: geth.Namespace}, cm); err != nil {
		return false, err
	}
	original := cm.Data["genesis.json"]
	patched, err := patchGenesisExtraData(original, geth.Status.AccountAddress)
	if err != nil {
		logger.Error(err, "修改 extraData 失败")
		return false, err
	}

	cm.Data["genesis.json"] = patched
	if err := r.Update(ctx, cm); err != nil {
		return false, err
	}
	return true, nil
}

func (r *GethReconciler) ensureDeployment(ctx context.Context, geth *xttv1.Geth) (bool, error) {
	logger := logf.FromContext(ctx)
	depName := geth.Name
	dep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: geth.Namespace,
		Name:      geth.Name,
	}, dep); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("准备创建 Geth 节点 Deployment")
			dep := buildDeployment(geth)
			if err := ctrl.SetControllerReference(geth, dep, r.Scheme); err != nil {
				return false, err
			}
			if err := r.Create(ctx, dep); err != nil {
				return false, err
			}

			_ = updateStatusPhase(ctx, r.Status(), r.Recorder, geth, "Running", "Ready")

			logger.Info("Deployment 已创建", "deployment", depName)
			return true, nil
		} else {
			return false, err
		}
	} else {
		return false, err
	}
}

func (r *GethReconciler) ensureSvc(ctx context.Context, geth *xttv1.Geth) (bool, error) {
	logger := logf.FromContext(ctx)
	svc := &corev1.Service{}
	svcName := geth.Name
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: geth.Namespace,
		Name:      geth.Name,
	}, svc); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("准备创建 Geth Service", "svc", svcName)
			svc := buildService(geth)
			if err := ctrl.SetControllerReference(geth, svc, r.Scheme); err != nil {
				return false, err
			}

			if err := r.Create(ctx, svc); err != nil {
				return false, err
			}
			logger.Info("Service 创建完成", "svc", svcName)
			return true, nil
		} else {
			return false, err
		}
	} else {
		return false, nil
	}
}
