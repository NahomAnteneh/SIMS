package collector

import (
	"github.com/infrastructure/sims/internal/domain"
)

// CompactBatch collapses multiple updates to the same VM within a single batch
// into one final record, keyed by instance_uuid or vcenter_id:moref,
// applying deterministic field-merge rules.
func CompactBatch(records []domain.VMRecord) []domain.VMRecord {
	if len(records) <= 1 {
		return records
	}

	keyToIndex := make(map[string]int)
	morefToIndex := make(map[string]int)
	compacted := make([]domain.VMRecord, 0, len(records))

	for _, incoming := range records {
		uuidKey := incoming.InstanceUUID
		morefKey := incoming.VCenterID + ":" + incoming.MoRef

		idx := -1
		if uuidKey != "" {
			if i, ok := keyToIndex[uuidKey]; ok {
				idx = i
			}
		}
		if idx == -1 && incoming.MoRef != "" {
			if i, ok := morefToIndex[morefKey]; ok {
				idx = i
			}
		}

		if idx == -1 {
			idx = len(compacted)
			compacted = append(compacted, incoming)
			if uuidKey != "" {
				keyToIndex[uuidKey] = idx
			}
			if incoming.MoRef != "" {
				morefToIndex[morefKey] = idx
			}
			continue
		}

		// Merge incoming into existing record at idx
		existing := compacted[idx]
		merged := mergeRecords(existing, incoming)
		compacted[idx] = merged
		if merged.InstanceUUID != "" {
			keyToIndex[merged.InstanceUUID] = idx
		}
		if merged.MoRef != "" {
			morefToIndex[merged.VCenterID+":"+merged.MoRef] = idx
		}
	}

	return compacted
}

// mergeRecords applies deterministic field-level precedence:
// incoming values overwrite existing values when defined.
func mergeRecords(existing, incoming domain.VMRecord) domain.VMRecord {
	out := existing

	// Lifecycle / Deletion precedence
	if incoming.DeletedAt != nil {
		out.DeletedAt = incoming.DeletedAt
	} else if existing.DeletedAt != nil && incoming.DeletedAt == nil {
		// Recreated or undeleted VM in same batch
		out.DeletedAt = nil
	}

	// Always take latest vCenter / MoRef (handles xvMotion identity migrations)
	if incoming.MoRef != "" {
		out.MoRef = incoming.MoRef
	}
	if incoming.VCenterID != "" {
		out.VCenterID = incoming.VCenterID
	}
	if incoming.BiosUUID != "" {
		out.BiosUUID = incoming.BiosUUID
	}
	if incoming.Name != "" {
		out.Name = incoming.Name
	}
	if incoming.PowerState != "" {
		out.PowerState = incoming.PowerState
	}
	if incoming.ConnectionState != "" {
		out.ConnectionState = incoming.ConnectionState
	}
	if incoming.CPUCores > 0 {
		out.CPUCores = incoming.CPUCores
	}
	if incoming.MemoryMB > 0 {
		out.MemoryMB = incoming.MemoryMB
	}
	if incoming.GuestOS != "" {
		out.GuestOS = incoming.GuestOS
	}
	if incoming.GuestHostName != "" {
		out.GuestHostName = incoming.GuestHostName
	}
	if incoming.GuestIPAddress != "" {
		out.GuestIPAddress = incoming.GuestIPAddress
	}
	if incoming.Annotation != "" {
		out.Annotation = incoming.Annotation
	}
	if incoming.FolderID != "" {
		out.FolderID = incoming.FolderID
	}
	if incoming.FolderPath != "" {
		out.FolderPath = incoming.FolderPath
	}
	if incoming.HostID != "" {
		out.HostID = incoming.HostID
	}
	if incoming.ClusterID != "" {
		out.ClusterID = incoming.ClusterID
	}
	if incoming.DatacenterID != "" {
		out.DatacenterID = incoming.DatacenterID
	}

	// Associations: replace with newest set if non-empty
	if len(incoming.DatastoreMoRefs) > 0 {
		out.DatastoreMoRefs = incoming.DatastoreMoRefs
		out.DatastoreIDs = incoming.DatastoreIDs
	}
	if len(incoming.NetworkInterfaces) > 0 {
		out.NetworkInterfaces = incoming.NetworkInterfaces
	}
	if incoming.Tags != nil {
		out.Tags = incoming.Tags
	}
	if incoming.CustomAttributes != nil {
		out.CustomAttributes = incoming.CustomAttributes
	}

	return out
}
