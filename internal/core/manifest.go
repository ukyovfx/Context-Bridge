package core

import (
	"encoding/json"
	"errors"
	"fmt"
)

func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, errors.New("manifest is invalid")
	}
	if manifest.Generator.Name != "contextbridge" || !manifest.Lifecycle.CreatedByContextBridge {
		return Manifest{}, errors.New("manifest is not a Context Bridge manifest")
	}
	switch manifest.SchemaVersion {
	case 1:
		if manifest.Project.Name == "" || manifest.Project.Repository == "" {
			return Manifest{}, errors.New("Manifest V1 project metadata is invalid")
		}
	case 2:
		if err := ValidateID(manifest.Project.ID, ProjectIDPrefix); err != nil {
			return Manifest{}, err
		}
		if manifest.Repository == nil {
			return Manifest{}, errors.New("Manifest V2 repository identity is missing")
		}
		if err := ValidateID(manifest.Repository.ID, RepositoryIDPrefix); err != nil {
			return Manifest{}, err
		}
		if manifest.Repository.CanonicalBranch == "" {
			return Manifest{}, errors.New("Manifest V2 canonical branch is missing")
		}
		if !manifest.Lifecycle.LocalOnly {
			if err := manifest.Repository.Identity.Validate(); err != nil {
				return Manifest{}, fmt.Errorf("Manifest V2 repository identity: %w", err)
			}
			if manifest.Repository.PrimaryRemoteName == "" {
				return Manifest{}, errors.New("Manifest V2 primary remote is missing")
			}
		}
	default:
		return Manifest{}, errors.New("manifest schema_version is unsupported")
	}
	return manifest, nil
}
