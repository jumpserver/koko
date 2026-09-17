package guacd

import (
	"fmt"
)

type GuacamoleStatus struct {
	HttpCode int
	WsCode   int
	GuaCode  int
}

func (g GuacamoleStatus) String() string {
	return fmt.Sprintf("%d - %d - %d", g.HttpCode,
		g.WsCode, g.GuaCode)
}

func (g GuacamoleStatus) Error() string {
	return g.String()
}

var (
	/**
	 * The operation succeeded.
	 */
	StatusSuccess GuacamoleStatus = GuacamoleStatus{200, 1000, 0x0000}

	/**
	 * The requested operation is unsupported.
	 */
	StatusUnsupported GuacamoleStatus = GuacamoleStatus{501, 1011, 0x0100}

	/**
	 * The operation could not be performed due to an internal failure.
	 */
	StatusServerError GuacamoleStatus = GuacamoleStatus{500, 1011, 0x0200}

	/**
	 * The operation could not be performed as the server is busy.
	 */
	StatusServerBusy GuacamoleStatus = GuacamoleStatus{503, 1008, 0x0201}

	/**
	 * The operation could not be performed because the upstream server is not
	 * responding.
	 */
	StatusUpstreamTimeout GuacamoleStatus = GuacamoleStatus{504, 1011, 0x0202}

	/**
	 * The operation was unsuccessful due to an error or otherwise unexpected
	 * condition of the upstream server.
	 */
	StatusUpstreamError GuacamoleStatus = GuacamoleStatus{502, 1011, 0x0203}

	/**
	 * The operation could not be performed as the requested resource does not
	 * exist.
	 */
	StatusResourceNotFound GuacamoleStatus = GuacamoleStatus{404, 1002, 0x0204}

	/**
	 * The operation could not be performed as the requested resource is already
	 * in use.
	 */
	StatusResourceConflict GuacamoleStatus = GuacamoleStatus{409, 1008, 0x0205}

	/**
	 * The operation could not be performed as the requested resource is now
	 * closed.
	 */
	StatusResourceClosed GuacamoleStatus = GuacamoleStatus{404, 1002, 0x0206}

	/**
	 * The operation could not be performed because the upstream server does
	 * not appear to exist.
	 */
	StatusUpstreamNotFound GuacamoleStatus = GuacamoleStatus{502, 1011, 0x0207}

	/**
	 * The operation could not be performed because the upstream server is not
	 * available to service the request.
	 */
	StatusUpstreamUnavailable GuacamoleStatus = GuacamoleStatus{502, 1011, 0x0208}

	/**
	 * The session within the upstream server has ended because it conflicted
	 * with another session.
	 */
	StatusSessionConflict GuacamoleStatus = GuacamoleStatus{409, 1008, 0x0209}

	/**
	 * The session within the upstream server has ended because it appeared to
	 * be inactive.
	 */
	StatusSessionTimeout GuacamoleStatus = GuacamoleStatus{408, 1002, 0x020A}

	/**
	 * The session within the upstream server has been forcibly terminated.
	 */
	StatusSessionClosed GuacamoleStatus = GuacamoleStatus{404, 1002, 0x020B}

	/**
	 * The operation could not be performed because bad parameters were given.
	 */
	StatusClientBadRequest GuacamoleStatus = GuacamoleStatus{400, 1002, 0x0300}

	/**
	 * Permission was denied to perform the operation, as the user is not yet
	 * authorized (not yet logged in, for example). As HTTP 401 has implications
	 * for HTTP-specific authorization schemes, this status continues to map to
	 * HTTP 403 ("Forbidden"). To do otherwise would risk unintended effects.
	 */
	StatusClientUnauthorized GuacamoleStatus = GuacamoleStatus{403, 1008, 0x0301}

	/**
	 * Permission was denied to perform the operation, and this operation will
	 * not be granted even if the user is authorized.
	 */
	StatusClientForbidden GuacamoleStatus = GuacamoleStatus{403, 1008, 0x0303}

	/**
	 * The client took too long to respond.
	 */
	StatusClientTimeout GuacamoleStatus = GuacamoleStatus{408, 1002, 0x0308}

	/**
	 * The client sent too much data.
	 */
	StatusClientOverRun GuacamoleStatus = GuacamoleStatus{413, 1009, 0x030D}

	/**
	 * The client sent data of an unsupported or unexpected type.
	 */
	StatusClientBadType GuacamoleStatus = GuacamoleStatus{415, 1003, 0x030F}

	/**
	 * The operation failed because the current client is already using too
	 * many resources.
	 */
	StatusClientTooMany GuacamoleStatus = GuacamoleStatus{429, 1008, 0x031D}
)
