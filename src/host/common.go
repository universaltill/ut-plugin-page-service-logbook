package host

// Fail-closed ports: the capability does not exist on this till yet.

type unavailableDB struct{}

func (unavailableDB) Exec(string, ...any) (int64, error)  { return 0, ErrUnavailable }
func (unavailableDB) Query(string, ...any) ([]Row, error) { return nil, ErrUnavailable }

type unavailableRecords struct{}

func (unavailableRecords) Forward(string, string, any) (bool, error) { return false, ErrUnavailable }
func (unavailableRecords) Role() (string, error)                     { return "", ErrUnavailable }
func (unavailableRecords) Status(string) ([]RecordStatus, error)     { return nil, ErrUnavailable }
func (unavailableRecords) SnapshotSet(string, any) (bool, error)     { return false, ErrUnavailable }
func (unavailableRecords) SnapshotGet(string, any) (bool, error)     { return false, ErrUnavailable }
