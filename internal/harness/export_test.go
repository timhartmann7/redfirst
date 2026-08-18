package harness

// ReadResults opens the results grammar to a test. Driving every shape of a
// JUnit or TAP file through a real hook run would say nothing about the parser
// and cost a second a case.
func ReadResults(path string) ([]Case, error) { return readResults(path) }
