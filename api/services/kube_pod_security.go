package services

import (
	corev1 "k8s.io/api/core/v1"
)

func boolPtr(v bool) *bool { return &v }

func ensureEnvVar(c *corev1.Container, name string, value string) {
	if c == nil || name == "" {
		return
	}
	for _, ev := range c.Env {
		if ev.Name == name {
			return
		}
	}
	c.Env = append(c.Env, corev1.EnvVar{Name: name, Value: value})
}

// Matches the uid used by distroless "nonroot" images.
// We set this explicitly in strict mode so images that default to USER 0 don't fail
// kubelet's runAsNonRoot check ("image will run as root") before the container starts.
const tenantDefaultUID int64 = 65532

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
// strict=false (compat fallback — Render-compatible):
// - seccomp RuntimeDefault (pod-level)
// - automountServiceAccountToken=false (pod-level)
// - allowPrivilegeEscalation=false + privileged=false (container-level)
// - capabilities.drop: NET_RAW, MKNOD, SYS_CHROOT, SETFCAP (container-level)
// - root filesystem writable, container's default UID (usually 0)
func applyTenantSecurityContext(pod *corev1.PodSpec, c *corev1.Container, strict bool, dockerAccess ...bool) {
	hasDinD := len(dockerAccess) > 0 && dockerAccess[0]
	if pod != nil {
		if pod.SecurityContext == nil {
			pod.SecurityContext = &corev1.PodSecurityContext{}
		}
		// When Docker-in-Docker sidecar is present, skip seccomp profile — DinD needs unconfined.
		if !hasDinD && pod.SecurityContext.SeccompProfile == nil {
			pod.SecurityContext.SeccompProfile = &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			}
		}
		// When DinD sidecar is present, skip pod-level UID/GID constraints — the sidecar must run as root.
		if strict && !hasDinD {
			if pod.SecurityContext.RunAsNonRoot == nil {
				pod.SecurityContext.RunAsNonRoot = boolPtr(true)
			}
			if pod.SecurityContext.RunAsUser == nil {
				pod.SecurityContext.RunAsUser = int64Ptr(tenantDefaultUID)
			}
			if pod.SecurityContext.RunAsGroup == nil {
				pod.SecurityContext.RunAsGroup = int64Ptr(tenantDefaultUID)
			}
			if pod.SecurityContext.FSGroup == nil {
				pod.SecurityContext.FSGroup = int64Ptr(tenantDefaultUID)
			}
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
		// Many runtimes (npm/corepack, Python, etc) attempt to write under $HOME/.cache. In strict mode
		// the root filesystem is read-only, so point "home-ish" dirs at our writable /tmp mount.
		ensureEnvVar(c, "HOME", "/tmp")
		ensureEnvVar(c, "XDG_CACHE_HOME", "/tmp/.cache")
		ensureEnvVar(c, "COREPACK_HOME", "/tmp/.corepack")
		ensureEnvVar(c, "NPM_CONFIG_CACHE", "/tmp/.npm")
		return
	}

	// Compat mode: drop dangerous capabilities that web apps never need.
	c.SecurityContext.Capabilities = &corev1.Capabilities{
		Drop: []corev1.Capability{"NET_RAW", "MKNOD", "SYS_CHROOT", "SETFCAP"},
	}

	// Prevent user pods from accessing the Kubernetes API.
	if pod != nil && pod.AutomountServiceAccountToken == nil {
		pod.AutomountServiceAccountToken = boolPtr(false)
	}
}
