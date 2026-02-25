{{/*
Return the API version for DRA resources (DeviceClass, ResourceClaim, etc).

Kubernetes 1.34+ serves these as resource.k8s.io/v1.
Older clusters may still serve resource.k8s.io/v1beta1.
*/}}
{{- define "helm-charts-k8s.draApiVersion" -}}
{{- if .Capabilities.APIVersions.Has "resource.k8s.io/v1" -}}
resource.k8s.io/v1
{{- else if .Capabilities.APIVersions.Has "resource.k8s.io/v1beta2" -}}
resource.k8s.io/v1beta2
{{- else if .Capabilities.APIVersions.Has "resource.k8s.io/v1beta1" -}}
resource.k8s.io/v1beta1
{{- end -}}
{{- end }}
