package updater

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

// Code is a typed error code. The set is closed except for http_<status>,
// which is built from a three-digit status. A code is the only description of
// a failure that reaches an answer, a log line or the audit file.
type Code string

// The error codes and outcomes of the update helper (ADR 0010 U2).
const (
	CodeBadRequest           Code = "bad_request"
	CodeBusy                 Code = "busy"
	CodeNotFound             Code = "not_found"
	CodeSignatureInvalid     Code = "signature_invalid"
	CodeChecksumMismatch     Code = "checksum_mismatch"
	CodeManifestInvalid      Code = "manifest_invalid"
	CodeDowngradeRefused     Code = "downgrade_refused"
	CodeAlreadyInstalled     Code = "already_installed"
	CodeRequirementsNotMet   Code = "requirements_not_met"
	CodeHelperTooOld         Code = "helper_too_old"
	CodeInstallerRequired    Code = "installer_required"
	CodeConfirmationRequired Code = "confirmation_required"
	CodeKeyChanged           Code = "key_changed"
	CodeUnitRejected         Code = "unit_rejected"
	CodeComponentsDamaged    Code = "components_damaged"
	CodeStageExpired         Code = "stage_expired"
	CodeNoSpace              Code = "no_space"
	CodeCurrentUnhealthy     Code = "current_unhealthy"
	CodeCooldown             Code = "cooldown"
	CodeIOError              Code = "io_error"
	CodeInternal             Code = "internal"
)

// The codes of the service itself (ADR 0010 U8 for the network codes).
const (
	CodeUnavailable     Code = "unavailable"      // the helper could not be reached
	CodeNotChecked      Code = "not_checked"      // no check result to apply
	CodeVersionMismatch Code = "version_mismatch" // not the version of the last check
	CodeVersionUnknown  Code = "version_unknown"  // the installed version is not a release version
	CodeTooLarge        Code = "too_large"        // a file is larger than its cap
	CodeCacheError      Code = "cache_error"      // the cache directory could not be used
	CodeBadResponse     Code = "bad_response"     // GitHub or the helper answered with something unusable
	CodeDNSFailure      Code = "dns_failure"
	CodeTLSFailure      Code = "tls_failure"
	CodeTimeout         Code = "timeout"
	CodeUnreachable     Code = "unreachable"
	CodeRateLimited     Code = "rate_limited"
	CodeRedirectRefused Code = "redirect_refused" // a redirect left the allow-list or exceeded the limit
	CodeCancelled       Code = "cancelled"        // the service is shutting down
)

var texts = map[Code]string{
	CodeBadRequest:           "The request is not valid.",
	CodeBusy:                 "Another update operation is running.",
	CodeNotFound:             "The requested item was not found.",
	CodeSignatureInvalid:     "The release signature is not valid.",
	CodeChecksumMismatch:     "A checksum does not match.",
	CodeManifestInvalid:      "The release manifest is not valid.",
	CodeDowngradeRefused:     "The release is older than the installed version.",
	CodeAlreadyInstalled:     "This version is already installed.",
	CodeRequirementsNotMet:   "This device does not meet the release requirements.",
	CodeHelperTooOld:         "The installed helper is too old for this release.",
	CodeInstallerRequired:    "This release must be installed with the installer.",
	CodeConfirmationRequired: "An explicit confirmation is required.",
	CodeKeyChanged:           "The release changes the trusted key.",
	CodeUnitRejected:         "A unit file was rejected.",
	CodeComponentsDamaged:    "The installed helper components are damaged.",
	CodeStageExpired:         "The verified release has expired.",
	CodeNoSpace:              "There is not enough free space.",
	CodeCurrentUnhealthy:     "The installed version is not healthy.",
	CodeCooldown:             "Try again later.",
	CodeIOError:              "A storage error occurred.",
	CodeInternal:             "An internal error occurred.",
	CodeUnavailable:          "The update helper could not be reached.",
	CodeNotChecked:           "Check for updates first.",
	CodeVersionMismatch:      "This is not the version that was found by the last check.",
	CodeVersionUnknown:       "The installed version is not a release version.",
	CodeTooLarge:             "A file is larger than allowed.",
	CodeCacheError:           "The download cache could not be used.",
	CodeBadResponse:          "The answer could not be used.",
	CodeDNSFailure:           "The name of the update server could not be resolved.",
	CodeTLSFailure:           "The secure connection to the update server failed. Check the date and time of the device.",
	CodeTimeout:              "The update server did not answer in time.",
	CodeUnreachable:          "The update server could not be reached.",
	CodeRateLimited:          "The update server refuses more requests for now.",
	CodeRedirectRefused:      "The update server sent the request somewhere that is not allowed.",
	CodeCancelled:            "The operation was cancelled.",
}

// Text is the fixed text of a code. It never contains anything that came from
// the network, from GitHub or from the helper.
func Text(c Code) string {
	if t, ok := texts[c]; ok {
		return t
	}
	if _, ok := httpStatus(c); ok {
		return "The update server answered with an error."
	}
	return texts[CodeInternal]
}

// httpCode is the code for an HTTP status of GitHub (ADR 0010 U8: http_<status>).
func httpCode(status int) Code {
	if status < 100 || status > 599 {
		return CodeBadResponse
	}
	return Code("http_" + strconv.Itoa(status))
}

func httpStatus(c Code) (int, bool) {
	s, ok := strings.CutPrefix(string(c), "http_")
	if !ok || len(s) != 3 {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 100 && n <= 599
}

// Error is the one error type of the package. Error() is the code and its fixed
// text. Differs is set for installer_required only, from the closed list of
// component names. RetryAfter is set for rate_limited and cooldown-like waits.
type Error struct {
	Code       Code
	Differs    []string
	RetryAfter time.Duration
}

func (e *Error) Error() string { return string(e.Code) + ": " + Text(e.Code) }

func fail(c Code) *Error { return &Error{Code: c} }

// codeOf returns the code of err, or internal for anything that is not an
// *Error. A Go error string is never passed on.
func codeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternal
}

// errRedirect marks a refused redirect inside the HTTP client.
var errRedirect = errors.New("redirect refused")

// classifyNet maps an error of the HTTP client to a code. Go's error strings
// contain host names and addresses; only the code leaves this function.
func classifyNet(ctx context.Context, err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	switch {
	case errors.Is(err, errRedirect):
		return fail(CodeRedirectRefused)
	case errors.Is(err, context.Canceled):
		if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
			return fail(CodeCancelled)
		}
		return fail(CodeUnreachable)
	case errors.Is(err, context.DeadlineExceeded):
		return fail(CodeTimeout)
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return fail(CodeDNSFailure)
	}
	var (
		unknownAuth x509.UnknownAuthorityError
		certInvalid x509.CertificateInvalidError
		hostErr     x509.HostnameError
		verifyErr   *tls.CertificateVerificationError
		recordHdr   tls.RecordHeaderError
		alert       tls.AlertError
	)
	if errors.As(err, &unknownAuth) || errors.As(err, &certInvalid) || errors.As(err, &hostErr) ||
		errors.As(err, &verifyErr) || errors.As(err, &recordHdr) || errors.As(err, &alert) {
		return fail(CodeTLSFailure)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return fail(CodeTimeout)
	}
	return fail(CodeUnreachable)
}
