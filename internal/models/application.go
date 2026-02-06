package models

// ApplicationChangeType indicates whether an application is new, existing, or being deleted.
type ApplicationChangeType string

const (
	// ApplicationExisting indicates the app exists in ArgoCD
	ApplicationExisting ApplicationChangeType = "existing"
	// ApplicationNew indicates the app is being added in this PR
	ApplicationNew ApplicationChangeType = "new"
	// ApplicationDeleted indicates the app is being deleted in this PR
	ApplicationDeleted ApplicationChangeType = "deleted"
)

// Application represents an Argo CD application with relevant metadata.
type Application struct {
	Name                 string                `json:"name"`
	Namespace            string                `json:"namespace"`
	Project              string                `json:"project"`
	RepoURL              string                `json:"repoURL"`
	Path                 string                `json:"path"`
	TargetRevision       string                `json:"targetRevision"`
	DestinationServer    string                `json:"destinationServer"`
	DestinationNamespace string                `json:"destinationNamespace"`
	SyncStatus           SyncStatus            `json:"syncStatus"`
	HealthStatus         HealthStatus          `json:"healthStatus"`
	Sources              []ApplicationSource   `json:"sources,omitempty"`
	ApplicationSetName   string                `json:"applicationSetName,omitempty"`
	Labels               map[string]string     `json:"labels,omitempty"`
	AutoSyncEnabled      bool                  `json:"autoSyncEnabled"`
	ChangeType           ApplicationChangeType `json:"changeType,omitempty"`
	SourceFile           string                `json:"sourceFile,omitempty"` // File path where this app CR is defined
}

// ApplicationSource represents a source in a multi-source application.
type ApplicationSource struct {
	RepoURL        string      `json:"repoURL"`
	Path           string      `json:"path,omitempty"`
	TargetRevision string      `json:"targetRevision"`
	Chart          string      `json:"chart,omitempty"`
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
	HealthStatusHealthy     HealthStatus = "Healthy"
	HealthStatusDegraded    HealthStatus = "Degraded"
	HealthStatusProgressing HealthStatus = "Progressing"
	HealthStatusSuspended   HealthStatus = "Suspended"
	HealthStatusMissing     HealthStatus = "Missing"
	HealthStatusUnknown     HealthStatus = "Unknown"
)

// ApplicationSet represents an Argo CD ApplicationSet.
type ApplicationSet struct {
	Name      string      `json:"name"`
	Namespace string      `json:"namespace"`
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

// ManifestDiff represents the diff between manifests.
// Supports 2-way (branch or live) and 3-way (both) comparisons.
type ManifestDiff struct {
	Resource    ResourceKey `json:"resource"`
	BaseState   string      `json:"baseState,omitempty"`  // State from base branch (for branch/both modes)
	LiveState   string      `json:"liveState,omitempty"`  // State from live cluster (for live/both modes)
	TargetState string      `json:"targetState"`          // State from target branch
	Diff        string      `json:"diff"`                 // Primary diff (branch diff for branch/both, live diff for live mode)
	BranchDiff  string      `json:"branchDiff,omitempty"` // Base → Target diff (for both mode)
	LiveDiff    string      `json:"liveDiff,omitempty"`   // Live → Target diff (for both mode)
	Action      DiffAction  `json:"action"`               // Action based on primary comparison
	LiveAction  DiffAction  `json:"liveAction,omitempty"` // Action for live comparison (for both mode)
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
	Application string           `json:"application"`
	Revision    string           `json:"revision"`
	Phase       SyncPhase        `json:"phase"`
	Message     string           `json:"message"`
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

// HasAutoSync returns true if the application has auto-sync enabled.
func (a *Application) HasAutoSync() bool {
	return a.AutoSyncEnabled
}

// IsNew returns true if this application is being created in the PR.
func (a *Application) IsNew() bool {
	return a.ChangeType == ApplicationNew
}

// IsDeleted returns true if this application is being deleted in the PR.
func (a *Application) IsDeleted() bool {
	return a.ChangeType == ApplicationDeleted
}

// IsExisting returns true if this application already exists in ArgoCD.
func (a *Application) IsExisting() bool {
	return a.ChangeType == "" || a.ChangeType == ApplicationExisting
}
