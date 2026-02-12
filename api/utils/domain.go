package utils

import (
	"fmt"
	"regexp"
	"strings"
)

var invalidDomainLabelChars = regexp.MustCompile(`[^a-z0-9-]+`)
var duplicateHyphens = regexp.MustCompile(`-+`)

// ServiceDomainLabel converts a service name into a DNS-safe label.
func ServiceDomainLabel(name string) string {
	label := strings.ToLower(strings.TrimSpace(name))
	label = strings.ReplaceAll(label, "_", "-")
	label = strings.ReplaceAll(label, ".", "-")
	label = invalidDomainLabelChars.ReplaceAllString(label, "-")
	label = duplicateHyphens.ReplaceAllString(label, "-")
	label = strings.Trim(label, "-")

	if label == "" {
		return "service"
	}

	if len(label) > 63 {
		label = strings.Trim(label[:63], "-")
		if label == "" {
			return "service"
		}
	}

	return label
}

// ServicePublicURL returns the externally reachable URL for public service types.
func ServicePublicURL(serviceType, serviceName, deployDomain string, hostPort int) string {
	switch serviceType {
	case "web", "static":
	default:
		return ""
	}

	domain := strings.ToLower(strings.TrimSpace(deployDomain))
	if domain != "" && domain != "localhost" {
		return "https://" + ServiceDomainLabel(serviceName) + "." + domain
	}

	if hostPort > 0 {
		return fmt.Sprintf("http://localhost:%d", hostPort)
	}

	return ""
}
