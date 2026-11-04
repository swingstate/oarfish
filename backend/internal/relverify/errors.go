package relverify

// Code is one of the error codes of the closed list in ADR 0010 U2. Only the
// codes this package can return are declared here; a test reads the ADR and
// requires every declared code to be in its list, so a code cannot be invented
// by accident.
type Code string

const (
	CodeBadRequest         Code = "bad_request"
	CodeSignatureInvalid   Code = "signature_invalid"
	CodeChecksumMismatch   Code = "checksum_mismatch"
	CodeManifestInvalid    Code = "manifest_invalid"
	CodeDowngradeRefused   Code = "downgrade_refused"
	CodeAlreadyInstalled   Code = "already_installed"
	CodeRequirementsNotMet Code = "requirements_not_met"
	CodeHelperTooOld       Code = "helper_too_old"
	CodeIOError            Code = "io_error"
	CodeInternal           Code = "internal"
)

// Text returns the fixed text of a code. The texts are the ones of the update
// helper's protocol (cmd/oarfish-update-helper), so that a caller can pass them
// on unchanged. An unknown code gets the text of CodeInternal.
func Text(c Code) string {
	switch c {
	case CodeBadRequest:
		return "The request is not valid."
	case CodeSignatureInvalid:
		return "The release signature is not valid."
	case CodeChecksumMismatch:
		return "A checksum does not match."
	case CodeManifestInvalid:
		return "The release manifest is not valid."
	case CodeDowngradeRefused:
		return "The release is older than the installed version."
	case CodeAlreadyInstalled:
		return "This version is already installed."
	case CodeRequirementsNotMet:
		return "This device does not meet the release requirements."
	case CodeHelperTooOld:
		return "The installed helper is too old for this release."
	case CodeIOError:
		return "A storage error occurred."
	}
	return "An internal error occurred."
}

// Error is the one error type of this package. Error() returns the code and
// its fixed text; Detail() returns a short fixed sentence for the log that says
// which rule failed. Neither ever contains bytes of the verified files, a file
// name taken from them, a path or an operating-system error string: every
// detail is a string literal (a test checks the source for that).
type Error struct {
	Code   Code
	detail string
}

func (e *Error) Error() string  { return string(e.Code) + ": " + Text(e.Code) }
func (e *Error) Detail() string { return e.detail }

// fail builds an *Error. The detail must be a string literal.
func fail(c Code, detail string) *Error { return &Error{Code: c, detail: detail} }
