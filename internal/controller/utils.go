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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
