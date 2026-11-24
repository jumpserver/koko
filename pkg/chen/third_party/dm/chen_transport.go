package dm

import "io"

// ChenTransportCloser snapshots only the underlying network transport. Call it
// under database/sql's physical-connection lock, before an operation starts.
// Closing this snapshot may run concurrently with network I/O; it does not close
// the driver, mutate statement maps or claim that the server canceled its SQL.
func (conn *DmConnection) ChenTransportCloser() io.Closer {
	if conn == nil || conn.Access == nil {
		return nil
	}
	return conn.Access.dm_build_3
}
