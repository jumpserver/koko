package engine

// Read original DRDA cancellation diagnostics only on the serialized console.
func db2CancellationCheckpoint(c *console) uint64 {
	var sequence uint64
	if c == nil || c.conn == nil {
		return 0
	}
	_ = c.conn.Raw(func(raw any) error {
		if probe, ok := raw.(interface{ ChenCancellationSequence() uint64 }); ok {
			sequence = probe.ChenCancellationSequence()
		}
		return nil
	})
	return sequence
}
func db2CancellationAcknowledged(c *console, checkpoint uint64) bool {
	return db2CancellationCheckpoint(c) > checkpoint
}
