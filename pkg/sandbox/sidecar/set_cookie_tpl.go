package sidecar

import (
	corev1 "k8s.io/api/core/v1"
)

func getHTTProbePath(pod *corev1.Pod) (probesPath []string) {
	for _, c := range pod.Spec.Containers {
		if c.LivenessProbe != nil && c.LivenessProbe.HTTPGet != nil {
			probesPath = append(probesPath, c.LivenessProbe.HTTPGet.Path)
		}
		if c.ReadinessProbe != nil && c.ReadinessProbe.HTTPGet != nil {
			probesPath = append(probesPath, c.ReadinessProbe.HTTPGet.Path)
		}
		if c.StartupProbe != nil && c.StartupProbe.HTTPGet != nil {
			probesPath = append(probesPath, c.StartupProbe.HTTPGet.Path)
		}
	}
	return probesPath
}
