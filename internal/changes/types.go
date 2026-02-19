package changes

// ChangeType classifies why a Terragrunt unit needs to be processed.
type ChangeType string

const (
	ModuleChanged    ChangeType = "ModuleChanged"
	ModuleAdded      ChangeType = "ModuleAdded"
	ModuleDeleted    ChangeType = "ModuleDeleted"
	EnvCommonChanged ChangeType = "EnvCommonChanged"
	SourceChanged    ChangeType = "SourceChanged"
)

// Change represents a single detected infrastructure change.
type Change struct {
	UnitPath string     `json:"unit_path"`
	Type     ChangeType `json:"type"`
	Files    []string   `json:"files"`
}
