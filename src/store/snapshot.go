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
	_, _ = s.h.Records().SnapshotSet("config", SnapConfig{MoneyEntry: s.EffectiveMode(), Categories: cats})
}

// Picker returns the staff a popup may log for. On a replica it is the last
// admin-pull snapshot; synced=false means none yet (the popup then offers
// nothing: no free-typed staff).
func (s *Store) Picker() (staff []Staff, synced bool, err error) {
	if !s.IsReplica() {
		l, err := s.ListStaff(false)
		return l, true, err
	}
	return s.snapshotStaff()
}

func (s *Store) snapshotStaff() (staff []Staff, found bool, err error) {
	var snap struct {
		Staff []SnapStaff `json:"staff"`
	}
	found, err = s.h.Records().SnapshotGet("staff", &snap)
	if err != nil || !found {
		return nil, false, err
	}
	for _, x := range snap.Staff {
		staff = append(staff, Staff{ID: x.ID, Name: x.Name, Colour: x.Colour, LinkedUserID: x.LinkedUserID, Status: "active"})
	}
	return staff, true, nil
}

// ActiveStaff is the member an entry may be logged for: on a replica a
// member of the staff snapshot (the replica's own staff table is never
// filled), on the main an active row of staff. ErrInvalid otherwise.
func (s *Store) ActiveStaff(id string) (Staff, error) {
	if id == "" {
		return Staff{}, ErrInvalid
	}
	if s.IsReplica() {
		list, found, err := s.snapshotStaff()
		if err != nil || !found {
			return Staff{}, ErrInvalid
		}
		for _, st := range list {
			if st.ID == id {
				return st, nil
			}
		}
		return Staff{}, ErrInvalid
	}
	st, err := s.GetStaff(id)
	if err != nil || st.Archived() {
		return Staff{}, ErrInvalid
	}
	return st, nil
}

// StaffForLabels names staff in lists: the snapshot on a replica, every
// member (archived included) on the main.
func (s *Store) StaffForLabels() ([]Staff, error) {
	if s.IsReplica() {
		l, _, err := s.snapshotStaff()
		return l, err
	}
	return s.ListStaff(true)
}
