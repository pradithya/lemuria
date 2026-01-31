package models

// Application represents an Argo CD application with relevant metadata.
type Application struct {
	Name             string            `json:"name"`
	Namespace        string            `json:"namespace"`
	Project          string            `json:"project"`
	RepoURL          string            `json:"repoURL"`
	Path             string            `json:"path"`
	TargetRevision   string            `json:"targetRevision"`
	DestinationServer string           `json:"destinationServer"`
	DestinationNamespace string        `json:"destinationNamespace"`
	SyncStatus       SyncStatus        `json:"syncStatus"`
	HealthStatus     HealthStatus      `json:"healthStatus"`
	Sources          []ApplicationSource `json:"sources,omitempty"`
	ApplicationSetName string          `json:"applicationSetName,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

// ApplicationSource represents a source in a multi-source application.
type ApplicationSource struct {
	RepoURL        string `json:"repoURL"`
	Path           string `json:"path,omitempty"`
	TargetRevision string `json:"targetRevision"`
	Chart          string `json:"chart,omitempty"`
	Helm           *HelmSource `json:"helm,omitempty"`
}

// HelmSource contains Helm-specific source configuration.
type HelmSource struct {
	ValueFiles []string `json:"valueFiles,omitempty"`
	Values     string   `json:"values,omitempty"`
}

// SyncStatus represents the sync state of an application.
type SyncStatus string

const (
	SyncStatusSynced    SyncStatus = "Synced"
	SyncStatusOutOfSync SyncStatus = "OutOfSync"
	SyncStatusUnknown   SyncStatus = "Unknown"
)

// HealthStatus represents the health state of an application.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "Healthy"
	HealthStatusDegraded  HealthStatus = "Degraded"
	HealthStatusProgressing HealthStatus = "Progressing"
	HealthStatusSuspended HealthStatus = "Suspended"
	HealthStatusMissing   HealthStatus = "Missing"
	HealthStatusUnknown   HealthStatus = "Unknown"
)

// ApplicationSet represents an Argo CD ApplicationSet.
type ApplicationSet struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Template  Application `json:"template"`
}

// Manifest represents a single Kubernetes manifest.
type Manifest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Raw        string `json:"raw"`
}

// ManifestDiff represents the diff between live and target manifests.
type ManifestDiff struct {
	Resource   ResourceKey `json:"resource"`
	LiveState  string      `json:"liveState"`
	TargetState string     `json:"targetState"`
	Diff       string      `json:"diff"`
	Action     DiffAction  `json:"action"`
}

// ResourceKey uniquely identifies a Kubernetes resource.
type ResourceKey struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
}

// String returns a human-readable resource identifier.
func (r ResourceKey) String() string {
	if r.Namespace != "" {
		return r.Namespace + "/" + r.Kind + "/" + r.Name
	}
	return r.Kind + "/" + r.Name
}

// DiffAction indicates what will happen to the resource during sync.
type DiffAction string

const (
	DiffActionCreate DiffAction = "create"
	DiffActionUpdate DiffAction = "update"
	DiffActionDelete DiffAction = "delete"
	DiffActionNone   DiffAction = "none"
)

// SyncResult represents the outcome of a sync operation.
type SyncResult struct {
	Application string          `json:"application"`
	Revision    string          `json:"revision"`
	Phase       SyncPhase       `json:"phase"`
	Message     string          `json:"message"`
	Resources   []ResourceResult `json:"resources"`
}

// SyncPhase represents the phase of a sync operation.
type SyncPhase string

const (
	SyncPhaseSucceeded SyncPhase = "Succeeded"
	SyncPhaseFailed    SyncPhase = "Failed"
	SyncPhaseRunning   SyncPhase = "Running"
	SyncPhaseError     SyncPhase = "Error"
)

// ResourceResult represents the sync result for a single resource.
type ResourceResult struct {
	Resource ResourceKey `json:"resource"`
	Status   string      `json:"status"`
	Message  string      `json:"message"`
}

// IsMultiSource returns true if the application has multiple sources.
func (a *Application) IsMultiSource() bool {
	return len(a.Sources) > 0
}

// GetRepoURLs returns all repository URLs for the application.
func (a *Application) GetRepoURLs() []string {
	if a.IsMultiSource() {
		urls := make([]string, len(a.Sources))
		for i, s := range a.Sources {
			urls[i] = s.RepoURL
		}
		return urls
	}
	return []string{a.RepoURL}
}
