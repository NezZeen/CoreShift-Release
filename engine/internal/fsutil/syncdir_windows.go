package fsutil

// syncDir does nothing on Windows: NTFS journals the rename itself, and a
// directory cannot be flushed through os.File.
func syncDir(string) {}
