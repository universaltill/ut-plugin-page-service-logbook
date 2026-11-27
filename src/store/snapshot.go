package store

// SnapStaff is a staff member as shared with replicas: no PINs, no roles,
// archived staff are not shared (ADR-0149, architect §5.5).
type SnapStaff struct {
	ID           string `json:"id"`
	Name         string `json:"display_name"`
	Colour       string `json:"colour"`
	LinkedUserID string `json:"linked_user_id"`
}

// SnapConfig is the settings replicas need.
type SnapConfig struct {
	MoneyEntry string   `json:"money_entry"`
	Categories []string `json:"service_categories"`
}

// PublishSnapshots writes the staff and config snapshots (main only; a
// replica's call is ignored by the host). Best effort: the next maintenance
// tick repeats it.
func (s *Store) PublishSnapshots() {
	list, err := s.ListStaff(false)
	if err != nil {
		return
	}
	out := make([]SnapStaff, 0, len(list))
	for _, st := range list {
		out = append(out, SnapStaff{st.ID, st.Name, st.Colour, st.LinkedUserID})
	}
	_, _ = s.h.Records().SnapshotSet("staff", map[string]any{"staff": out})
	cats := s.Categories()
	if cats == nil {
		cats = []string{}
	}
	_, _ = s.h.Records().SnapshotSet("config", SnapConfig{MoneyEntry: s.MoneyEntry(), Categories: cats})
}

// Picker returns the staff a popup may log for. On a replica it is the last
// admin-pull snapshot; synced=false means none yet (the popup then offers
// nothing: no free-typed staff).
func (s *Store) Picker() (staff []Staff, synced bool, err error) {
	role, _ := s.h.Records().Role()
	if role != "replica" {
		l, err := s.ListStaff(false)
		return l, true, err
	}
	var snap struct {
		Staff []SnapStaff `json:"staff"`
	}
	found, err := s.h.Records().SnapshotGet("staff", &snap)
	if err != nil || !found {
		return nil, false, err
	}
	for _, x := range snap.Staff {
		staff = append(staff, Staff{ID: x.ID, Name: x.Name, Colour: x.Colour, LinkedUserID: x.LinkedUserID, Status: "active"})
	}
	return staff, true, nil
}
