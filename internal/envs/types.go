package envs

// UnitInventory represents a discovered Terragrunt unit within an environment.
type UnitInventory struct {
	// LogicalName is the env-prefix-stripped path (e.g. "us-east-1/vpc").
	LogicalName string
	// FullPath is the filesystem path to the unit directory.
	FullPath string
	// Region is the detected region, if any.
	Region string
}

// StructuralDiff lists units that exist in one environment but not the other.
type StructuralDiff struct {
	OnlyInSource []UnitInventory
	OnlyInTarget []UnitInventory
}

// ContentDiff holds the line-by-line diff between a shared unit's terragrunt.hcl files.
type ContentDiff struct {
	LogicalName string
	SourcePath  string
	TargetPath  string
	Unified     string
	Identical   bool
}

// EnvDiffResult is the full comparison between two environments.
type EnvDiffResult struct {
	SourceEnv    string
	TargetEnv    string
	Structural   StructuralDiff
	ContentDiffs []ContentDiff
}

// PromotionPlan categorizes differences for promoting from source to target.
type PromotionPlan struct {
	SourceEnv     string
	TargetEnv     string
	NewUnits      []UnitInventory // units in source but not target
	ChangedUnits  []ContentDiff   // shared units with differences
	OrphanedUnits []UnitInventory // units in target but not source
}
