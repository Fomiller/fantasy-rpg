package gameserver

import (
	sdk "agones.dev/agones/sdks/go"
)

// runsList is the list on the Fleet's GameServer template.
const runsList = "runs"

type Agones struct {
	sdk *sdk.SDK
}

// NewAgones connects to the sidecar. It blocks for up to 30 s.
func NewAgones() (*Agones, error) {
	s, err := sdk.NewSDK()
	if err != nil {
		return nil, err
	}
	return &Agones{sdk: s}, nil
}

func (a *Agones) Ready() error                          { return a.sdk.Ready() }
func (a *Agones) Health() error                         { return a.sdk.Health() }
func (a *Agones) Shutdown() error                       { return a.sdk.Shutdown() }
func (a *Agones) SetAnnotation(key, value string) error { return a.sdk.SetAnnotation(key, value) }
func (a *Agones) RemoveRun(runID string) error          { return a.sdk.Beta().DeleteListValue(runsList, runID) }
func (a *Agones) Runs() ([]string, error)               { return a.sdk.Beta().GetListValues(runsList) }
func (a *Agones) SetRunCapacity(n int64) error          { return a.sdk.Beta().SetListCapacity(runsList, n) }

func (a *Agones) AddRun(runID string) error {
	listed, err := a.sdk.Beta().ListContains(runsList, runID)
	if err != nil || listed {
		return err
	}
	return a.sdk.Beta().AppendListValue(runsList, runID)
}
