package sidecar

import (
	"bytetrade.io/web3os/app-service/pkg/constants"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetSidecarConfigMap returns a configmap that data is envoy.yaml.
func GetSidecarConfigMap(configMapName, namespace, username string, injectPolicy, injectWs, injectUpload bool, appDomains []string, pod *corev1.Pod) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			constants.EnvoyConfigFileName: getEnvoyConfig(username, injectPolicy, injectWs, injectUpload, appDomains, pod),
		},
	}
}

// GetSidecarVolumeSpec returns the volume specification for a sidecar using the given configmap name.
func GetSidecarVolumeSpec(configMapName string) corev1.Volume {
	return corev1.Volume{
		Name: constants.SidecarConfigMapVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  constants.EnvoyConfigFileName,
						Path: constants.EnvoyConfigFileName,
					},
				},
			},
		},
	}
}
