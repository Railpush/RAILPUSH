package services

import (
	corev1 "k8s.io/api/core/v1"
)

func boolPtr(v bool) *bool { return &v }

func ensureWritableTmp(pod *corev1.PodSpec, c *corev1.Container) {
	if pod != nil {
		hasTmpVolume := false
		for _, v := range pod.Volumes {
			if v.Name == "tmp" {
				hasTmpVolume = true
				break
			}
		}
		if !hasTmpVolume {
			pod.Volumes = append(pod.Volumes, corev1.Volume{
				Name: "tmp",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
		}
	}

	if c == nil {
		return
	}

	hasTmpMount := false
	for _, vm := range c.VolumeMounts {
		if vm.MountPath == "/tmp" {
			hasTmpMount = true
			break
		}
	}
	if !hasTmpMount {
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name:      "tmp",
			MountPath: "/tmp",
		})
	}
}

// applyTenantSecurityContext applies tenant pod hardening.
//
// strict=true:
// - seccomp RuntimeDefault (pod-level)
// - runAsNonRoot=true (pod-level)
// - allowPrivilegeEscalation=false + privileged=false (container-level)
// - capabilities.drop=ALL (container-level)
// - readOnlyRootFilesystem=true + writable /tmp mount (container/pod-level)
//
// strict=false (compat fallback):
// - seccomp RuntimeDefault (pod-level)
// - allowPrivilegeEscalation=false + privileged=false (container-level)
// - capabilities.drop includes NET_RAW (container-level)
func applyTenantSecurityContext(pod *corev1.PodSpec, c *corev1.Container, strict bool) {
	if pod != nil {
		if pod.SecurityContext == nil {
			pod.SecurityContext = &corev1.PodSecurityContext{}
		}
		if pod.SecurityContext.SeccompProfile == nil {
			pod.SecurityContext.SeccompProfile = &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			}
		}
		if strict && pod.SecurityContext.RunAsNonRoot == nil {
			pod.SecurityContext.RunAsNonRoot = boolPtr(true)
		}
	}

	if c == nil {
		return
	}
	if c.SecurityContext == nil {
		c.SecurityContext = &corev1.SecurityContext{}
	}
	if c.SecurityContext.AllowPrivilegeEscalation == nil {
		c.SecurityContext.AllowPrivilegeEscalation = boolPtr(false)
	}
	if c.SecurityContext.Privileged == nil {
		c.SecurityContext.Privileged = boolPtr(false)
	}

	if strict {
		if c.SecurityContext.ReadOnlyRootFilesystem == nil {
			c.SecurityContext.ReadOnlyRootFilesystem = boolPtr(true)
		}
		c.SecurityContext.Capabilities = &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		}
		ensureWritableTmp(pod, c)
		return
	}

	if c.SecurityContext.Capabilities == nil {
		c.SecurityContext.Capabilities = &corev1.Capabilities{}
	}

	// Compatibility fallback: keep broad compatibility but at least drop raw-socket capability.
	drop := corev1.Capability("NET_RAW")
	for _, existing := range c.SecurityContext.Capabilities.Drop {
		if existing == drop {
			return
		}
	}
	c.SecurityContext.Capabilities.Drop = append(c.SecurityContext.Capabilities.Drop, drop)
}
