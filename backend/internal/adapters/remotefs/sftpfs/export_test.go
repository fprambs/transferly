package sftpfs

// SetBulkReads turns the second channel of strict servers on or off, and returns how to put it back.
func SetBulkReads(on bool) func() {
	before := bulkReads.Swap(on)
	return func() { bulkReads.Store(before) }
}
