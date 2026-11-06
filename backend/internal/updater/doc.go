// Package updater is the service side of the GUI update (ADR 0010, step G6.7):
// the GitHub client, the check with its shared in-memory result, the download
// into the cache, the advisory verification, the client of the update helper's
// socket and the one-job-at-a-time model that the routes of the server start.
//
// The service is unprivileged. It never gets a path, a URL or a command from
// the user: the only inputs of the routes are a version that must equal the
// version of the last check and a password. Everything that comes from GitHub,
// from the helper or from the network is untrusted input; it is checked
// against fixed patterns and closed sets and reaches answers, logs and the
// audit file only as one of the codes of this package, never as a Go error
// string (those contain host names and addresses).
//
// There is no timer and no goroutine that runs without a request: a check
// starts when a request asks for one, a job runs for the length of its work,
// and the transport closes every connection after its response, so no idle
// connection keeps a goroutine alive (A11.1). The result of a check lives in
// memory only; a restart of the service forgets it.
//
// Nothing here is verified on a device or against the real GitHub service.
package updater
