package services

import "strings"

// KubeBuildJobName returns the Kubernetes Job name used for Kaniko git builds for a deploy.
// It must stay in sync with the naming logic in BuildImageWithKaniko.
func KubeBuildJobName(deployID string) string {
	deployID = strings.TrimSpace(deployID)
	if deployID == "" {
		return ""
	}
	name := "rp-build-" + strings.ToLower(deployID)
	name = kubeNameInvalidChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	return name
}

