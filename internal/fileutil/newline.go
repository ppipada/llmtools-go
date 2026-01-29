package fileutil

// NewlineKind describes the newline convention detected in a file.
type NewlineKind string

const (
	NewlineLF   NewlineKind = "lf"
	NewlineCRLF NewlineKind = "crlf"
)

func (n NewlineKind) sep() string {
	if n == NewlineCRLF {
		return "\r\n"
	}
	return "\n"
}
