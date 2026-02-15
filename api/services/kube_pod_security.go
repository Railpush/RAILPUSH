package services

import (
	corev1 "k8s.io/api/core/v1"
)

func boolPtr(v bool) *bool { return &v }

// applyCompatSecurityContext applies "compatibility-first" pod hardening:
// - seccomp RuntimeDefault (pod-level)
// - no privilege escalation + drop NET_RAW (container-level)
//
// We intentionally do NOT force runAsNonRoot/runAsUser to avoid breaking common images.
func applyCompatSecurityContext(pod *corev1.PodSpec, c *corev1.Container) {
	if pod != nil {
		if pod.SecurityContext == nil {
			pod.SecurityContext = &corev1.PodSecurityContext{}
		}
		if pod.SecurityContext.SeccompProfile == nil {
			pod.SecurityContext.SeccompProfile = &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
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
	if c.SecurityContext.Capabilities == nil {
		c.SecurityContext.Capabilities = &corev1.Capabilities{}
	}

	// Basic hardening with minimal compatibility risk.
	// Dropping NET_RAW prevents raw socket usage (e.g., packet crafting) but keeps
	// common app capabilities (like NET_BIND_SERVICE) intact.
	drop := corev1.Capability("NET_RAW")
	for _, existing := range c.SecurityContext.Capabilities.Drop {
		if existing == drop {
			return
		}
	}
	c.SecurityContext.Capabilities.Drop = append(c.SecurityContext.Capabilities.Drop, drop)
}

