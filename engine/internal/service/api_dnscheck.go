package service

import (
	"errors"
	"net/http"
)

// dnsCheck is POST /v1/dnscheck: whether the DNS servers of the request
// answer (Service.CheckDNS). A server that is not an address the settings
// take is a 400; whether one answers is in the result.
func (a *api) dnsCheck(w http.ResponseWriter, r *http.Request) {
	var req DNSCheckRequest
	if !decode(w, r, &req) {
		return
	}
	res, err := a.svc.CheckDNS(r.Context(), req)
	switch {
	case errors.Is(err, ErrBadDNSServer):
		writeError(w, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, res)
	}
}
