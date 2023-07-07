package instrumentation

import (
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	cfinstr "github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
)

// StatWithDesc mirrors constraint framework instrumentation.Stat.
type StatWithDesc struct {
	cfinstr.Stat
	Description string `json:"description"`
}

// StatsEntryWithDesc mirrors constraint framework instrumentation.StatsEntry.
type StatsEntryWithDesc struct {
	// Scope is the level of granularity that the Stats
	// were created at.
	Scope string `json:"scope"`
	// StatsFor is the specific kind of Scope type that Stats
	// were created for.
	StatsFor string           `json:"statsFor"`
	Stats    []*StatWithDesc  `json:"stats"`
	Labels   []*cfinstr.Label `json:"labels,omitempty"`
}

// ToStatsEntriesWithDesc will use the client passed in to adorn constraint framework instrumentation.StatsEntry structs
// with a description and returns an array of StatsEntryWithDesc.
func ToStatsEntriesWithDesc(client *constraintclient.Client, cfentries []*cfinstr.StatsEntry) []*StatsEntryWithDesc {
	var entriesWithDesc []*StatsEntryWithDesc
	for _, se := range cfentries {
		sewd := &StatsEntryWithDesc{
			Scope:    se.Scope,
			StatsFor: se.StatsFor,
			Labels:   se.Labels,
		}

		var stats []*StatWithDesc
		for _, stat := range se.Stats {
			swd := &StatWithDesc{
				Stat: *stat,
			}

			var desc string
			if client != nil {
				desc = client.GetDescriptionForStat(stat.Source, stat.Name)
			} else {
				desc = cfinstr.UnknownDescription
			}

			swd.Description = desc
			stats = append(stats, swd)
		}

		sewd.Stats = stats
		entriesWithDesc = append(entriesWithDesc, sewd)
	}

	return entriesWithDesc
}
