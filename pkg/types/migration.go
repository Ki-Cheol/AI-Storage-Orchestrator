package types

import "time"

// MigrationRequest represents a pod migration request
type MigrationRequest struct {
	// Source pod information
	PodName      string `json:"pod_name" binding:"required"`
	PodNamespace string `json:"pod_namespace" binding:"required"`
	SourceNode   string `json:"source_node" binding:"required"`
	
	// Target node information  
	TargetNode string `json:"target_node" binding:"required"`
	
	// Migration options
	PreservePV    bool   `json:"preserve_pv,omitempty"`
	ForceRestart  bool   `json:"force_restart,omitempty"`
	Timeout       int    `json:"timeout,omitempty"` // seconds
}

// MigrationResponse represents the response for a migration request
type MigrationResponse struct {
	MigrationID string                 `json:"migration_id"`
	Status      MigrationStatus        `json:"status"`
	Message     string                 `json:"message"`
	Details     *MigrationDetails      `json:"details,omitempty"`
}

// MigrationStatus represents the current status of a migration
type MigrationStatus string

const (
	MigrationStatusPending    MigrationStatus = "pending"
	MigrationStatusRunning    MigrationStatus = "running"
	MigrationStatusCompleted  MigrationStatus = "completed"
	MigrationStatusFailed     MigrationStatus = "failed"
	MigrationStatusCancelled  MigrationStatus = "cancelled"
)

// MigrationDetails contains detailed information about the migration process
type MigrationDetails struct {
	StartTime     time.Time              `json:"start_time"`
	EndTime       *time.Time             `json:"end_time,omitempty"`
	Duration      *time.Duration         `json:"duration,omitempty"`
	
	// Resource usage before migration
	OriginalResources *ResourceUsage     `json:"original_resources,omitempty"`
	// Resource usage after migration  
	OptimizedResources *ResourceUsage    `json:"optimized_resources,omitempty"`
	
	// Container status information
	ContainerStates []ContainerState    `json:"container_states,omitempty"`
	
	// PV checkpoint information
	CheckpointPath  string             `json:"checkpoint_path,omitempty"`
	PVClaimName     string             `json:"pv_claim_name,omitempty"`
	
	// New pod information after migration
	NewPodName      string             `json:"new_pod_name,omitempty"`
}

// ResourceUsage represents CPU and memory usage
type ResourceUsage struct {
	CPUUsage    float64 `json:"cpu_usage"`    // CPU cores
	MemoryUsage int64   `json:"memory_usage"` // bytes
	Timestamp   time.Time `json:"timestamp"`
}

// ContainerState represents the state of a container during migration
type ContainerState struct {
	Name        string `json:"name"`
	State       string `json:"state"`       // waiting, running, completed  
	RestartCount int32  `json:"restart_count"`
	ShouldMigrate bool  `json:"should_migrate"` // whether this container should be migrated
}

// MigrationMetrics represents performance metrics for migrations
type MigrationMetrics struct {
	TotalMigrations    int64         `json:"total_migrations"`
	SuccessfulMigrations int64       `json:"successful_migrations"`
	FailedMigrations   int64         `json:"failed_migrations"`
	AverageDuration    time.Duration `json:"average_duration"`
	CPUSavings         float64       `json:"cpu_savings_percentage"`
	MemorySavings      float64       `json:"memory_savings_percentage"`
}
