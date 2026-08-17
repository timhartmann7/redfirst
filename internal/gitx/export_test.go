package gitx

// ParseHunkHeader opens the hunk grammar to a test. Git produces a malformed
// header for nobody, so a fixture repository cannot reach these branches, and
// the grammar is the one place where the parser takes git's word for the shape
// of what follows.
func ParseHunkHeader(line string) (start, added, deleted int, err error) {
	h, err := parseHunkHeader(line)
	return h.start, h.added, h.deleted, err
}
